package smb

import (
	"errors"
	stdfs "io/fs"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// --- TID → share and wire-path → store-path resolution. Every FS command first
// binds its request to the share the TID names, then turns the wire filename into
// a store path through the share's codec (which threads the per-request charset).
// Resolution lives here so the handlers do not each re-derive it. ---

// treeFor resolves the request header's TID to the bound disk share. It returns
// the share, or an NTSTATUS to refuse with: STATUS_SMB_BAD_TID for an unknown
// TID, STATUS_ACCESS_DENIED for an IPC$ tree (the FS engine serves no pipes), and
// statusSuccess when sh is set. The share is returned by held pointer, so a share
// removed from the Manager mid-session still serves in-flight requests (the
// RemoveShare contract).
func (s *Service) treeFor(sess *smbSession, h protocol.Header) (sh *Share, status uint32) {
	tc, ok := sess.tree(h.TID)
	if !ok {
		return nil, statusSMBBadTID
	}
	if tc.ipc || tc.share == nil {
		return nil, statusAccessDenied // no named-pipe filesystem
	}
	return tc.share, statusSuccess
}

// extractWirePath pulls the filename wire bytes from a request byte area for a
// path-bearing command. A leading 0x04 SMB_FORMAT_ASCII buffer-format byte (CORE
// dialect path ops carry it) is stripped; the remaining bytes up to the
// charset's NUL terminator are the path, still in the wire charset (UTF-16LE when
// the Unicode flag is set, OEM/ANSI otherwise). Returns the raw path bytes and
// the number of bytes consumed (so a two-path command — RENAME — can read the
// next one).
func extractWirePath(area []byte, flags2 uint16) (path []byte, consumed int, ok bool) {
	if len(area) == 0 {
		return nil, 0, false
	}
	off := 0
	if area[0] == 0x04 { // SMB_FORMAT_ASCII buffer-format prefix
		off = 1
	}
	rest := area[off:]
	if wireFor(flags2) == fs.WireUTF16 {
		// UTF-16LE: a path may need a leading pad byte to 2-byte-align after the
		// odd buffer-format byte; the terminator is a 00 00 unit on an even
		// boundary.
		if off == 1 && len(rest) > 0 {
			rest = rest[1:] // alignment pad following the 0x04
			off++
		}
		for i := 0; i+1 < len(rest); i += 2 {
			if rest[i] == 0 && rest[i+1] == 0 {
				return rest[:i], off + i + 2, true
			}
		}
		return rest, off + len(rest), true
	}
	if nul := indexByte(rest, 0); nul >= 0 {
		return rest[:nul], off + nul + 1, true
	}
	return rest, off + len(rest), true
}

// resolvePath turns a request's filename byte area into a store path through the
// share's codec, returning the store path or an NTSTATUS to refuse with. A name
// the store charset cannot represent yields STATUS_OBJECT_NAME_INVALID; an
// unsupported wire charset yields STATUS_OBJECT_NAME_INVALID as well (the client
// asked for a charset the share's codec does not implement). An empty area yields
// statusObjectNameNotFound.
func resolvePath(sh *Share, area []byte, flags2 uint16) (store string, status uint32) {
	raw, _, ok := extractWirePath(area, flags2)
	if !ok {
		return "", statusObjectNameNotFound
	}
	p, err := sh.ResolvePath(raw, flags2)
	if err != nil {
		return "", statusObjectNameInvalid
	}
	return p, statusSuccess
}

// storeParent splits a '/'-separated store path into its parent dir and leaf.
func storeParent(store string) (parent, leaf string) {
	i := strings.LastIndex(store, "/")
	if i < 0 {
		return "", store
	}
	return store[:i], store[i+1:]
}

// mapFSErr maps a FileSystem error to the NTSTATUS to return. ENOSPC and other
// platform errnos stay an OS-adapter refinement (core is syscall-free); an
// unrecognised error is STATUS_UNSUCCESSFUL rather than a leaked Go error string.
func mapFSErr(err error) uint32 {
	switch {
	case err == nil:
		return statusSuccess
	case errors.Is(err, stdfs.ErrNotExist):
		return statusObjectNameNotFound
	case errors.Is(err, stdfs.ErrExist):
		return statusObjectNameCollision
	case errors.Is(err, stdfs.ErrPermission):
		return statusAccessDenied
	case errors.Is(err, fs.ErrUnrepresentable):
		return statusObjectNameInvalid
	default:
		return statusUnsuccessful
	}
}
