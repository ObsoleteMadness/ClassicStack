package smb

import (
	"errors"
	stdfs "io/fs"
	"os"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"

	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- SMB_COM_NT_CREATE_ANDX (0xA2): the NT/2000/XP open-or-create path. Unlike
// the Win9x OPEN_ANDX, an NT client opens files AND directories through this one
// command, selecting behaviour through CreateDisposition (open/create/overwrite)
// and CreateOptions (the FILE_DIRECTORY_FILE / FILE_NON_DIRECTORY_FILE intent).
// It resolves the wire path through the share codec and acts only via sh.FS(), so
// it holds no storage-layout knowledge — the data fork is the file itself, the
// AppleDouble/ADS/xattr container is the AFP side's concern over the same
// ForkEngine. These mirror the [MS-CIFS] §2.2.4.64 wire layout. ---

// NT CreateDisposition values ([MS-CIFS] §2.2.4.64.1): what to do given the
// file's existence.
const (
	ntDispositionSupersede   uint32 = 0 // overwrite if present, else create
	ntDispositionOpen        uint32 = 1 // open existing; fail if missing
	ntDispositionCreate      uint32 = 2 // create new; fail if present
	ntDispositionOpenIf      uint32 = 3 // open if present, else create
	ntDispositionOverwrite   uint32 = 4 // open+truncate if present; fail if missing
	ntDispositionOverwriteIf uint32 = 5 // open+truncate if present, else create
)

// NT CreateOptions bits the engine honours ([MS-CIFS] §2.2.4.64.1). The rest
// (write-through, sequential-only, etc.) are advisory and ignored.
const (
	ntOptionDirectoryFile    uint32 = 0x00000001 // must be a directory
	ntOptionNonDirectoryFile uint32 = 0x00000040 // must not be a directory
)

// NT CreateAction values returned in the response ([MS-CIFS] §2.2.4.64.2).
const (
	ntActionSuperseded uint32 = 0 // existing file superseded
	ntActionOpened     uint32 = 1 // existing file opened
	ntActionCreated    uint32 = 2 // new file created
	ntActionOverwexp   uint32 = 3 // existing file overwritten
)

// handleNtCreateAndX answers SMB_COM_NT_CREATE_ANDX (0xA2). It binds the TID to a
// disk share, resolves the wire path, then opens or creates a file or directory
// per CreateDisposition/CreateOptions, granting a FID. The name is carried in the
// BCC area (length given by the NameLength word), Unicode-padded to a 2-byte
// boundary after the odd parameter block when the Unicode flag is set.
//
// Request words ([MS-CIFS] §2.2.4.64.1, WCT=24): AndXCommand(1) AndXReserved(1)
// AndXOffset(2) Reserved(1) NameLength(2) Flags(4) RootDirectoryFID(4)
// DesiredAccess(4) AllocationSize(8) ExtFileAttributes(4) ShareAccess(4)
// CreateDisposition(4) CreateOptions(4) ImpersonationLevel(4) SecurityFlags(1).
func (s *Service) handleNtCreateAndX(sess *smbSession, h protocol.Header, req []byte) []byte {
	sh, st := s.treeFor(sess, h)
	if st != statusSuccess {
		return errResponse(h, st)
	}
	words, area, ok := reqBody(req)
	if !ok || len(words) < 48 { // WCT=24 → 48 param bytes
		return errResponse(h, statusObjectNameInvalid)
	}
	desiredAccess := bp.LE32(words[15:19])
	disposition := bp.LE32(words[35:39])
	options := bp.LE32(words[39:43])

	store, st := resolvePath(sh, area, h.Flags2)
	if st != statusSuccess {
		return errResponse(h, st)
	}

	wantDir := options&ntOptionDirectoryFile != 0
	wantNonDir := options&ntOptionNonDirectoryFile != 0

	// Probe the existing object so disposition + options can be reconciled against
	// reality before any mutation.
	info, statErr := sh.FS().Stat(store)
	exists := statErr == nil

	if exists && info.IsDir() && wantNonDir {
		return errResponse(h, statusFileIsADirectory)
	}
	if exists && !info.IsDir() && wantDir {
		return errResponse(h, statusNotADirectory)
	}

	// Disposition gating against existence.
	switch disposition {
	case ntDispositionOpen, ntDispositionOverwrite:
		if !exists {
			return errResponse(h, statusObjectNameNotFound)
		}
	case ntDispositionCreate:
		if exists {
			return errResponse(h, statusObjectNameCollision)
		}
	case ntDispositionOpenIf, ntDispositionOverwriteIf, ntDispositionSupersede:
		// open-or-create dispositions: no existence gate — created if missing,
		// (over)written if present.
	}

	if wantDir {
		return s.ntCreateDir(sess, h, sh, store, exists, info, desiredAccess)
	}
	return s.ntCreateFile(sess, h, sh, store, exists, desiredAccess, disposition)
}

// ntCreateDir opens or creates a directory handle. A directory FID carries no open
// fork.File — directory operations (enumeration) run over the share FS by path —
// so the handle records isDir and the store path only.
func (s *Service) ntCreateDir(sess *smbSession, h protocol.Header, sh *Share, store string, exists bool, info stdfs.FileInfo, desiredAccess uint32) []byte {
	action := ntActionOpened
	if !exists {
		if err := sh.FS().CreateDir(store); err != nil {
			return errResponse(h, mapFSErr(err))
		}
		action = ntActionCreated
		fresh, err := sh.FS().Stat(store)
		if err != nil {
			return errResponse(h, mapFSErr(err))
		}
		info = fresh
	}
	fid := sess.allocFID(&fileHandle{share: sh, path: store, isDir: true})
	return buildNtCreateResponse(h, fid, action, info, true, sh.AttrsFor(store, info), desiredAccess)
}

// ntCreateFile opens or creates a regular-file handle, applying the truncate
// semantics of the overwrite/supersede dispositions. The returned FID maps to the
// open data fork.
func (s *Service) ntCreateFile(sess *smbSession, h protocol.Header, sh *Share, store string, exists bool, desiredAccess, disposition uint32) []byte {
	truncate := disposition == ntDispositionOverwrite ||
		disposition == ntDispositionOverwriteIf ||
		disposition == ntDispositionSupersede
	writable := ntWantsWrite(desiredAccess) || truncate || !exists

	flag := os.O_RDONLY
	if writable {
		flag = os.O_RDWR
	}
	if truncate {
		flag |= os.O_TRUNC
	}

	var (
		f      fs.File
		err    error
		action = ntActionOpened
	)
	if exists {
		f, err = sh.FS().OpenFile(store, flag)
		if err != nil {
			return errResponse(h, mapFSErr(err))
		}
		if truncate && disposition != ntDispositionSupersede {
			action = ntActionOverwexp
		} else if disposition == ntDispositionSupersede {
			action = ntActionSuperseded
		}
	} else {
		f, err = sh.FS().CreateFile(store)
		if err != nil {
			if errors.Is(err, stdfs.ErrNotExist) {
				return errResponse(h, statusObjectPathNotFound)
			}
			return errResponse(h, mapFSErr(err))
		}
		action = ntActionCreated
	}

	fresh, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		return errResponse(h, statusUnsuccessful)
	}
	fid := sess.allocFID(&fileHandle{share: sh, file: f, path: store, writable: writable})
	return buildNtCreateResponse(h, fid, action, fresh, false, sh.AttrsFor(store, fresh), desiredAccess)
}

// ntWantsWrite reports whether an NT DesiredAccess mask requests any write right
// (FILE_WRITE_DATA | APPEND | WRITE_ATTRS/EA | GENERIC_WRITE | GENERIC_ALL), so a
// read-only intent opens a read-only handle the write path then refuses.
func ntWantsWrite(desiredAccess uint32) bool {
	const (
		fileWriteData  = 0x00000002
		fileAppendData = 0x00000004
		fileWriteEA    = 0x00000010
		fileWriteAttrs = 0x00000100
		genericWrite   = 0x40000000
		genericAll     = 0x10000000
		maximumAllowed = 0x02000000
	)
	return desiredAccess&(fileWriteData|fileAppendData|fileWriteEA|fileWriteAttrs|genericWrite|genericAll|maximumAllowed) != 0
}

// buildNtCreateResponse packs the SMB_COM_NT_CREATE_ANDX reply (WCT=34): the andx
// terminator, oplock level (none), FID, create action, the four NT timestamps, ext
// attributes, allocation + end-of-file sizes, file type, device state, and the
// directory flag ([MS-CIFS] §2.2.4.64.2).
func buildNtCreateResponse(h protocol.Header, fid uint16, action uint32, info stdfs.FileInfo, isDir bool, attrs uint16, _ uint32) []byte {
	w := make([]byte, 68) // WCT=34 → 68 param bytes
	w[0] = protocol.CommandNoAndXCommand
	// w[1] AndXReserved, w[2:4] AndXOffset — left 0 (no chained command).
	w[4] = 0 // OpLockLevel = none
	bp.PutLE16(w[5:7], fid)
	bp.PutLE32(w[7:11], action)

	ft := fileTime(info.ModTime())
	bp.PutLE64(w[11:19], ft) // CreationTime
	bp.PutLE64(w[19:27], ft) // LastAccessTime
	bp.PutLE64(w[27:35], ft) // LastWriteTime
	bp.PutLE64(w[35:43], ft) // ChangeTime

	bp.PutLE32(w[43:47], uint32(attrs)) // ExtFileAttributes
	size := uint64(info.Size())
	bp.PutLE64(w[47:55], allocSize(size, isDir)) // AllocationSize
	bp.PutLE64(w[55:63], size)                   // EndOfFile
	bp.PutLE16(w[63:65], 0)                      // FileType = disk
	bp.PutLE16(w[65:67], 0)                      // DeviceState
	if isDir {
		w[67] = 1 // Directory
	}
	return reply(h, statusSuccess, 34, w, nil)
}
