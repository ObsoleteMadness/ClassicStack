/*
Package afp implements the AppleTalk Filing Protocol (AFP) 2.x.

AFP is an application-layer protocol that allows users to share files and network
resources.

Inside Macintosh: Networking, Chapter 9.
https://dev.os9.ca/techpubs/mac/Networking/Networking-223.html
*/
package afp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/pkg/cnid"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

// AFP Commands.
// Inside Macintosh: Networking.
const (
	FPByteRangeLock   = 1  // lock byte ranges in an open fork.
	FPCloseVol        = 2  // notify server that a workstation no longer needs a volume.
	FPCloseDir        = 3  // close a directory on a variable Directory ID volume.
	FPCloseFork       = 4  // close an open fork.
	FPCopyFile        = 5  // copy a file from one server volume to another.
	FPCreateDir       = 6  // create a new directory.
	FPCreateFile      = 7  // create a new file.
	FPDelete          = 8  // delete a file or empty directory.
	FPEnumerate       = 9  // list files and directories within a directory.
	FPFlush           = 10 // flush data associated with a volume to disk.
	FPFlushFork       = 11 // write an open fork's internal buffers to disk.
	FPGetDirParms     = 12
	FPGetFileParms    = 13
	FPGetForkParms    = 14 // read an open fork's parameters.
	FPGetSrvrInfo     = 15 // get server information (name, version strings, UAMs, flags) without opening a session.
	FPGetSrvrParms    = 16 // get list of server volumes after a session is established.
	FPGetVolParms     = 17 // get parameters for a given volume.
	FPLogin           = 18 // authenticate user and establish a session.
	FPLoginCont       = 19 // continue multi-step user authentication process.
	FPLogout          = 20 // terminate an AFP session.
	FPMapID           = 21 // map user or group ID to the corresponding name.
	FPMapName         = 22 // map user or group name to the corresponding ID.
	FPMoveAndRename   = 23 // move and optionally rename a file or directory to a different parent directory.
	FPOpenVol         = 24 // request access to a volume, optionally providing a password.
	FPOpenDir         = 25 // open a directory on a variable Directory ID volume to retrieve its Directory ID.
	FPOpenFork        = 26 // open a data or resource fork of an existing file.
	FPGetSrvrMsg      = 38
	FPRead            = 27 // read data from an open fork.
	FPRename          = 28 // rename a file or directory.
	FPSetDirParms     = 29 // change parameters of a specified directory.
	FPSetFileParms    = 30 // change parameters of a specified file.
	FPSetForkParms    = 31 // change parameters of an open fork.
	FPSetVolParms     = 32 // change parameters of a specified volume.
	FPWrite           = 33 // write data to an open fork.
	FPGetFileDirParms = 34 // get parameters associated with a given file or directory.
	FPSetFileDirParms = 35 // set parameters common to both files and directories.
	FPChangePassword  = 36 // change a user's password.
	FPGetUserInfo     = 37 // retrieve information about a user (AFP 2.0+).

	// AFP 2.2 additions.
	FPExchangeFiles = 42

	// AFP 2.1 catalogued search.
	FPCatSearch = 43

	// AFP 2.0+ Desktop Database commands (Inside Macintosh: Networking §C).
	// Finder uses these to store/retrieve icons, application mappings, and comments.
	FPOpenDT        = 48  // open the Desktop database for access.
	FPCloseDT       = 49  // close access to the Desktop database.
	FPGetIcon       = 51  // retrieve a specific icon bitmap from the Desktop database.
	FPGetIconInfo   = 52  // get description or determine set of icons for an application.
	FPAddAPPL       = 53  // register an application mapping (APPL) in the Desktop database.
	FPRemoveAPPL    = 54  // remove an application mapping from the Desktop database.
	FPGetAPPL       = 55  // get an application mapping from the Desktop database.
	FPAddComment    = 56  // add or replace a Finder comment for a file or directory.
	FPRemoveComment = 57  // remove a Finder comment for a file or directory.
	FPGetComment    = 58  // retrieve a Finder comment for a file or directory.
	FPAddIcon       = 192 // add a new icon bitmap to the Desktop database. (special: maps to ASPUserWrite)
)

// forkHandle tracks an open fork (data or resource).
type forkHandle struct {
	file           File // nil for an empty resource fork
	isRsrc         bool
	rsrcOff        int64  // offset within the AppleDouble file where resource data starts
	rsrcLen        int64  // current length of resource fork data
	rsrcLenFieldAt int64  // file offset of the ResourceFork entry's length field in the AppleDouble header
	filePath       string // absolute path of the file whose fork is open
	volID          uint16 // volume this fork belongs to
}

type byteRangeLock struct {
	lockKey   string
	ownerFork uint16
	start     int64
	length    int64 // -1 means open-ended (to EOF)
}

const defaultMaxByteRangeLocks = 4096

// AFPService implements AppleTalk Filing Protocol.
type AFPService struct {
	ServerName  string
	Volumes     []Volume
	fs          FileSystem
	volumeFS    map[uint16]FileSystem
	meta        ForkMetadataBackend            // global override when ForkMetadataBackend is injected via options
	metas       map[uint16]ForkMetadataBackend // per-volume backends (keyed by Volume.ID)
	mu          sync.RWMutex
	options     AFPOptions
	cnidStores  map[uint16]CNIDStore
	desktopDB   DesktopDBBackend
	forks       map[uint16]*forkHandle
	nextFork    uint16
	byteLocks   []byteRangeLock
	maxReadSize int // transport quantum limit; 0 = unlimited
	maxLocks    int

	users       map[string]string // map[username]password
	nextSRefNum uint16

	// volumeBackupDate stores AFP "backup date" (ADouble-style seconds since 1904)
	// per volume, as set by FPSetVolParms (AFP 2.x §5.1.32).
	volumeBackupDate map[uint16]uint32

	// Desktop database state — one DesktopDB per volume (persists across sessions).
	desktopDBs map[uint16]DesktopDB
	dtRefs     map[uint16]uint16 // DTRefNum → volume ID
	nextDTRef  uint16

	transports []Transport
	dumper     service.PacketDumper
}

func (s *AFPService) SetPacketDumper(dumper service.PacketDumper) {
	s.dumper = dumper
}

// SetMaxReadSize caps FPRead ReqCount to n bytes and propagates the same limit
// to any filesystem that supports range limiting (e.g. MacGardenFileSystem).
// ASP calls this with its quantum size so HTTP range requests from virtual
// filesystems never exceed what one ASP reply can carry. DSI leaves it at 0.
func (s *AFPService) SetMaxReadSize(n int) {
	s.maxReadSize = n
	type rangeLimiter interface{ SetMaxRangeSize(int) }
	if rl, ok := s.fs.(rangeLimiter); ok {
		rl.SetMaxRangeSize(n)
	}
	for _, vfs := range s.volumeFS {
		if rl, ok := vfs.(rangeLimiter); ok {
			rl.SetMaxRangeSize(n)
		}
	}
}

func (s *AFPService) logPacket(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if s.dumper != nil {
		s.dumper.LogPacket(msg)
	}
}

func NewAFPService(serverName string, configs []VolumeConfig, fs FileSystem, transports []Transport, opts ...AFPOptions) *AFPService {
	options := DefaultAFPOptions()
	if len(opts) > 0 {
		options = opts[0]
	}

	s := &AFPService{
		ServerName:  serverName,
		fs:          fs,
		volumeFS:    make(map[uint16]FileSystem),
		options:     options,
		cnidStores:  make(map[uint16]CNIDStore),
		desktopDB:   resolveDesktopDBBackend(options),
		forks:       make(map[uint16]*forkHandle),
		nextFork:    1,
		byteLocks:   make([]byteRangeLock, 0),
		maxLocks:    defaultMaxByteRangeLocks,
		users:       make(map[string]string),
		nextSRefNum: 1,

		volumeBackupDate: make(map[uint16]uint32),

		desktopDBs: make(map[uint16]DesktopDB),
		dtRefs:     make(map[uint16]uint16),
		nextDTRef:  1,

		transports: transports,
	}

	if options.ForkMetadataBackend != nil {
		// Test injection: single global backend for all volumes.
		s.meta = options.ForkMetadataBackend
	} else {
		// Normal path: build a per-volume backend using each volume's AppleDoubleMode
		// (falling back to options.AppleDoubleMode if the volume does not specify one).
		s.metas = make(map[uint16]ForkMetadataBackend)
	}

	cnidBackend := resolveCNIDBackend(options)
	usedVolumeIDs := make(map[uint16]struct{}, len(configs))
	for i, cfg := range configs {
		volumeID := uint16(i + 1)
		if options.PersistentVolumeIDs {
			volumeID = persistentVolumeIDForConfig(cfg, usedVolumeIDs)
		} else {
			usedVolumeIDs[volumeID] = struct{}{}
		}
		volume := Volume{
			Config: cfg,
			ID:     volumeID,
		}
		s.Volumes = append(s.Volumes, volume)
		store := cnidBackend.Open(volume)
		store.EnsureReserved(filepath.Clean(cfg.Path), CNIDRoot)
		s.cnidStores[volume.ID] = store

		if fs != nil {
			s.volumeFS[volume.ID] = fs
		}
		if s.volumeFS[volume.ID] == nil {
			if backend, err := newBackendForVolumeConfig(cfg); err == nil {
				s.volumeFS[volume.ID] = backend
			}
		}

		if s.metas != nil {
			metaFS := s.volumeFS[volume.ID]
			if metaFS == nil {
				metaFS = fs
			}
			if metaFS != nil {
				mode := cfg.AppleDoubleMode
				if mode == "" {
					mode = options.AppleDoubleMode
				}
				s.metas[volume.ID] = NewAppleDoubleBackend(metaFS, mode, options.DecomposedFilenames)
			}
		}
	}
	go s.rebuildDesktopDBsIfConfigured()
	return s
}

func persistentVolumeIDForConfig(cfg VolumeConfig, used map[uint16]struct{}) uint16 {
	nameKey := strings.ToLower(strings.TrimSpace(cfg.Name))
	pathKey := filepath.Clean(strings.TrimSpace(cfg.Path))

	candidates := []string{
		nameKey,
		nameKey + "|" + pathKey,
	}
	for _, key := range candidates {
		id := crcVolumeID(key)
		if _, exists := used[id]; exists {
			continue
		}
		used[id] = struct{}{}
		return id
	}

	for salt := 1; ; salt++ {
		id := crcVolumeID(fmt.Sprintf("%s|%s|%d", nameKey, pathKey, salt))
		if _, exists := used[id]; exists {
			continue
		}
		used[id] = struct{}{}
		return id
	}
}

func crcVolumeID(key string) uint16 {
	id := uint16(crc32.ChecksumIEEE([]byte(key)) & 0xffff)
	if id == 0 {
		return 1
	}
	return id
}

// metaFor returns the ForkMetadataBackend for the given volume ID.
// If a per-volume backend is registered it is returned; otherwise the global
// injected backend (s.meta) is used. Returns nil when neither is available.
func (s *AFPService) metaFor(volID uint16) ForkMetadataBackend {
	if s.metas != nil {
		if m, ok := s.metas[volID]; ok {
			return m
		}
	}
	return s.meta
}

// metaForPath returns the ForkMetadataBackend for the volume whose root path
// is a prefix of path. Falls back to the global injected backend when no
// matching volume is found.
func (s *AFPService) metaForPath(path string) ForkMetadataBackend {
	clean := filepath.Clean(path)
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(vol.Config.Path, clean)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return s.metaFor(vol.ID)
		}
	}
	return s.meta
}

func (s *AFPService) fsForVolume(volID uint16) FileSystem {
	if fs, ok := s.volumeFS[volID]; ok && fs != nil {
		return fs
	}
	return s.fs
}

func (s *AFPService) fsForPath(path string) FileSystem {
	clean := filepath.Clean(path)
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(filepath.Clean(vol.Config.Path), clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if fs := s.fsForVolume(vol.ID); fs != nil {
				return fs
			}
		}
	}
	return s.fs
}

func newBackendForVolumeConfig(cfg VolumeConfig) (FileSystem, error) {
	fsType, err := NormalizeFSType(cfg.FSType)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(cfg.Path)
	switch fsType {
	case FSTypeLocalFS:
		return &LocalFileSystem{}, nil
	case FSTypeMacGarden:
		return NewMacGardenFileSystem(root), nil
	default:
		return nil, fmt.Errorf("unsupported fs_type %q", fsType)
	}
}

// Start initializes all underlying transports.
func (s *AFPService) Start(router service.Router) error {
	for _, t := range s.transports {
		if err := t.Start(router); err != nil {
			return err
		}
	}
	return nil
}

// Stop shuts down all underlying transports.
func (s *AFPService) Stop() error {
	var errs []error
	for _, t := range s.transports {
		if err := t.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("AFPService Stop errors: %v", errs)
	}
	return nil
}

// Socket returns the AppleTalk socket number if any of the transports listen on one.
// We return asp.ServerSocket (252) if we have a transport that needs it.
func (s *AFPService) Socket() uint8 {
	// The router expects services that listen on a specific socket to return it here.
	// Since AFPService wraps transports, we return the well-known ASP socket (252).
	// TCP-only instances won't be called for AppleTalk routing anyway if they don't register NBP.
	return 252 // asp.ServerSocket
}

// Inbound delegates inbound DDP packets to the underlying transports.
func (s *AFPService) Inbound(d ddp.Datagram, p port.Port) {
	for _, t := range s.transports {
		t.Inbound(d, p)
	}
}

// GetStatus implements the CommandHandler interface
func (s *AFPService) GetStatus() []byte {
	return BuildServerInfo(s.ServerName)
}

type Request interface {
	Unmarshal(data []byte) error
	String() string
}

type Response interface {
	Marshal() []byte
	String() string
}

func (s *AFPService) HandleCommand(data []byte) (resBytes []byte, errCode int32) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[AFP] PANIC in cmd=%d: %v\n%s", data[0], r, debug.Stack())
			resBytes = nil
			errCode = ErrParamErr
		}
	}()
	if len(data) == 0 {
		return nil, ErrParamErr
	}

	cmd := data[0]
	afpCommandsTotal.Inc()

	var req Request
	var handler func(Request) (Response, int32)

	switch cmd {
	case FPGetSrvrInfo:
		req = &FPGetSrvrInfoReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetSrvrInfo(req.(*FPGetSrvrInfoReq))
			if err != nil {
				return nil, ErrMiscErr
			}
			return res, NoErr
		}
	case FPGetSrvrParms:
		req = &FPGetSrvrParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetSrvrParms(req.(*FPGetSrvrParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPLogin:
		req = &FPLoginReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleLogin(req.(*FPLoginReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPLogout:
		req = &FPLogoutReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleLogout(req.(*FPLogoutReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPOpenVol:
		req = &FPOpenVolReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleOpenVol(req.(*FPOpenVolReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPGetVolParms:
		req = &FPGetVolParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetVolParms(req.(*FPGetVolParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPOpenDir:
		req = &FPOpenDirReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleOpenDir(req.(*FPOpenDirReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPCloseVol:
		req = &FPCloseVolReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleCloseVol(req.(*FPCloseVolReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPCloseDir:
		req = &FPCloseDirReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleCloseDir(req.(*FPCloseDirReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPCloseFork:
		req = &FPCloseForkReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleCloseFork(req.(*FPCloseForkReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPFlush:
		req = &FPFlushReq{}
		handler = func(req Request) (Response, int32) {
			return s.handleFlush(req.(*FPFlushReq))
		}
	case FPFlushFork:
		req = &FPFlushForkReq{}
		handler = func(req Request) (Response, int32) {
			return s.handleFlushFork(req.(*FPFlushForkReq))
		}
	case FPEnumerate:
		req = &FPEnumerateReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleEnumerate(req.(*FPEnumerateReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPGetFileDirParms:
		req = &FPGetFileDirParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetFileDirParms(req.(*FPGetFileDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPOpenFork:
		req = &FPOpenForkReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleOpenFork(req.(*FPOpenForkReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPRead:
		req = &FPReadReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleRead(req.(*FPReadReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPWrite:
		req = &FPWriteReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleWrite(req.(*FPWriteReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPCreateFile:
		req = &FPCreateFileReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleCreateFile(req.(*FPCreateFileReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPCreateDir:
		req = &FPCreateDirReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleCreateDir(req.(*FPCreateDirReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPDelete:
		req = &FPDeleteReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleDelete(req.(*FPDeleteReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	case FPRename:
		req = &FPRenameReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleRename(req.(*FPRenameReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}
	// --- Commands with minimal compatibility implementations ---

	case FPByteRangeLock: // byte-range locking (Finder uses during copy)
		req = &FPByteRangeLockReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleByteRangeLock(req.(*FPByteRangeLockReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPCopyFile:
		req = &FPCopyFileReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleCopyFile(req.(*FPCopyFileReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPGetDirParms:
		req = &FPGetDirParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetDirParms(req.(*FPGetDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPGetFileParms:
		req = &FPGetFileParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetFileParms(req.(*FPGetFileParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPGetForkParms:
		req = &FPGetForkParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleGetForkParms(req.(*FPGetForkParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPLoginCont: // TODO: Implement second-phase UAM login (AFP 2.x §5.1.19)
		req = &FPLoginContReq{}
		handler = func(req Request) (Response, int32) {
			log.Printf("[AFP] TODO: Implement FPLoginCont called — not implemented")
			return nil, ErrCallNotSupported
		}

	case FPMapID:
		req = &FPMapIDReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleMapID(req.(*FPMapIDReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPMapName:
		req = &FPMapNameReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleMapName(req.(*FPMapNameReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPMoveAndRename:
		req = &FPMoveAndRenameReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleMoveAndRename(req.(*FPMoveAndRenameReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPSetDirParms:
		req = &FPSetDirParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleSetDirParms(req.(*FPSetDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPSetFileParms:
		req = &FPSetFileParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleSetFileParms(req.(*FPSetFileParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPSetForkParms:
		req = &FPSetForkParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleSetForkParms(req.(*FPSetForkParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPSetVolParms:
		req = &FPSetVolParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleSetVolParms(req.(*FPSetVolParmsReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPSetFileDirParms:
		req = &FPSetFileDirParmsReq{}
		handler = func(req Request) (Response, int32) {
			res, err := s.handleSetFileDirParms(req.(*FPSetFileDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		}

	case FPExchangeFiles:
		req = &FPExchangeFilesReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleExchangeFiles(req.(*FPExchangeFilesReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPGetSrvrMsg:
		req = &FPGetSrvrMsgReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleGetSrvrMsg(req.(*FPGetSrvrMsgReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPChangePassword: // changing passwords is not supported
		req = &FPUnsupportedReq{}
		handler = func(req Request) (Response, int32) {
			return nil, ErrCallNotSupported
		}

	case FPGetUserInfo: // user info not supported; full permissions assumed
		req = &FPUnsupportedReq{}
		handler = func(req Request) (Response, int32) {
			return nil, ErrCallNotSupported
		}

	case FPCatSearch: // TODO: Implement catalogued volume search (AFP 2.1)
		req = &FPCatSearchReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleCatSearch(req.(*FPCatSearchReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	// --- TODO Desktop Database commands (AFP 2.1+) ---
	// Finder uses the Desktop DB to store icons, application mappings (APPL tags),
	// and Get Info comments. Without this, icons fall back to generic defaults.

	case FPOpenDT: // open Desktop Database — create .AppleDesktop dir and .desktop.db cache
		req = &FPOpenDTReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleOpenDT(req.(*FPOpenDTReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPCloseDT: // close Desktop Database — invalidate DTRefNum
		req = &FPCloseDTReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleCloseDT(req.(*FPCloseDTReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPGetIcon: // retrieve icon bitmap from Desktop database
		req = &FPGetIconReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleGetIcon(req.(*FPGetIconReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPGetIconInfo: // retrieve icon metadata from Desktop database
		req = &FPGetIconInfoReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleGetIconInfo(req.(*FPGetIconInfoReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPAddIcon: // add icon bitmap to Desktop database
		req = &FPAddIconReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleAddIcon(req.(*FPAddIconReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPAddAPPL: // register APPL mapping in Desktop database
		req = &FPAddAPPLReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleAddAPPL(req.(*FPAddAPPLReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPRemoveAPPL: // remove APPL mapping from Desktop database
		req = &FPRemoveAPPLReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleRemoveAPPL(req.(*FPRemoveAPPLReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPGetAPPL: // retrieve APPL mapping from Desktop database
		req = &FPGetAPPLReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleGetAPPL(req.(*FPGetAPPLReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPAddComment: // add Finder comment to Desktop database
		req = &FPAddCommentReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleAddComment(req.(*FPAddCommentReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPRemoveComment: // remove Finder comment from Desktop database
		req = &FPRemoveCommentReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleRemoveComment(req.(*FPRemoveCommentReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	case FPGetComment: // retrieve Finder comment from Desktop database
		req = &FPGetCommentReq{}
		handler = func(req Request) (Response, int32) {
			res, errCode := s.handleGetComment(req.(*FPGetCommentReq))
			if res == nil {
				return nil, errCode
			}
			return res, errCode
		}

	default:
		log.Printf("[AFP] unknown command %d", cmd)
		return nil, ErrCallNotSupported
	}

	cmdData := data
	if cmd == FPLogin {
		// FPLoginReq.Unmarshal expects data without the command byte.
		cmdData = data[1:]
	}

	if err := req.Unmarshal(cmdData); err != nil {
		log.Printf("[AFP] Error unmarshaling cmd %d: %v", cmd, err)
		return nil, ErrParamErr
	}

	s.logPacket("[AFP] → %s", req.String())
	s.logResolvedPaths(req)

	var res Response
	res, errCode = handler(req)

	if res != nil {
		s.logPacket("[AFP] ← %s (err=%d)", res.String(), errCode)
		resBytes = res.Marshal()
	} else if errCode != NoErr {
		s.logPacket("[AFP] ← cmd=%d err=%d", cmd, errCode)
	}

	return resBytes, errCode
}

func (s *AFPService) logResolvedPaths(req Request) {
	switch r := req.(type) {
	case *FPOpenDirReq:
		s.logResolvedPath("FPOpenDir", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPEnumerateReq:
		s.logResolvedPath("FPEnumerate", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPGetFileDirParmsReq:
		s.logResolvedPath("FPGetFileDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPGetDirParmsReq:
		s.logResolvedPath("FPGetDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPGetFileParmsReq:
		s.logResolvedPath("FPGetFileParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPOpenForkReq:
		s.logResolvedPath("FPOpenFork", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPCreateFileReq:
		s.logResolvedPath("FPCreateFile", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPCreateDirReq:
		s.logResolvedPath("FPCreateDir", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPDeleteReq:
		s.logResolvedPath("FPDelete", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPSetDirParmsReq:
		s.logResolvedPath("FPSetDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPSetFileParmsReq:
		s.logResolvedPath("FPSetFileParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPSetFileDirParmsReq:
		s.logResolvedPath("FPSetFileDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPRenameReq:
		s.logResolvedPath("FPRename old", r.VolumeID, r.DirID, r.PathType, r.Name)
		s.logResolvedPath("FPRename new", r.VolumeID, r.DirID, r.NewPathType, r.NewName)
	case *FPMoveAndRenameReq:
		s.logResolvedPath("FPMoveAndRename src", r.VolumeID, r.SrcDirID, r.SrcPathType, r.SrcName)
		s.logResolvedPath("FPMoveAndRename dstDir", r.VolumeID, r.DstDirID, r.DstPathType, r.DstDirName)
	case *FPExchangeFilesReq:
		s.logResolvedPath("FPExchangeFiles src", r.VolumeID, r.SrcDirID, r.SrcPathType, r.SrcName)
		s.logResolvedPath("FPExchangeFiles dst", r.VolumeID, r.DstDirID, r.DstPathType, r.DstName)
	case *FPCopyFileReq:
		s.logResolvedPath("FPCopyFile src", r.SrcVolumeID, r.SrcDirID, r.SrcPathType, r.SrcName)
		s.logResolvedPath("FPCopyFile dstDir", r.DstVolumeID, r.DstDirID, r.DstPathType, r.DstDirName)
	case *FPAddAPPLReq:
		s.logResolvedPathFromDTRef("FPAddAPPL", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPRemoveAPPLReq:
		s.logResolvedPathFromDTRef("FPRemoveAPPL", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPAddCommentReq:
		s.logResolvedPathFromDTRef("FPAddComment", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPRemoveCommentReq:
		s.logResolvedPathFromDTRef("FPRemoveComment", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPGetCommentReq:
		s.logResolvedPathFromDTRef("FPGetComment", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPCatSearchReq:
		s.logResolvedPath("FPCatSearch", r.VolumeID, CNIDRoot, PathTypeLongNames, "")
	}
}

func (s *AFPService) logResolvedPath(op string, volumeID uint16, dirID uint32, pathType uint8, rawPath string) {
	resolved, errCode := s.resolveVolumePath(volumeID, dirID, rawPath, pathType)
	if errCode == NoErr {
		log.Printf("[AFP][Path] %s vol=%d dirID=%d pathType=%d raw=%q resolved=%q", op, volumeID, dirID, pathType, rawPath, resolved)
		return
	}
	log.Printf("[AFP][Path] %s vol=%d dirID=%d pathType=%d raw=%q unresolved err=%d", op, volumeID, dirID, pathType, rawPath, errCode)
}

func (s *AFPService) logResolvedPathFromDTRef(op string, dtRefNum uint16, dirID uint32, pathType uint8, rawPath string) {
	s.mu.RLock()
	volID, ok := s.dtRefs[dtRefNum]
	s.mu.RUnlock()
	if !ok {
		log.Printf("[AFP][Path] %s dtRef=%d dirID=%d pathType=%d raw=%q unresolved err=%d", op, dtRefNum, dirID, pathType, rawPath, ErrParamErr)
		return
	}
	s.logResolvedPath(op, volID, dirID, pathType, rawPath)
}

// statPathWithAppleDoubleFallback stats path and, if missing, retries with a
// "._" prefixed basename to support orphan AppleDouble files.
func (s *AFPService) statPathWithAppleDoubleFallback(path string) (string, fs.FileInfo, error) {
	m := s.metaForPath(path)
	if m == nil {
		return path, nil, os.ErrNotExist
	}
	return m.StatWithMetadataFallback(path)
}

// iconFileNameFor returns the host filesystem name for the Mac "Icon\r" file
// for the given volume, respecting its AppleDouble mode and decomposed filename settings.
func (s *AFPService) iconFileNameFor(volID uint16) string {
	if m := s.metaFor(volID); m != nil {
		return m.IconFileName()
	}
	if s.options.DecomposedFilenames {
		return "Icon0x0D"
	}
	return "Icon\r"
}

// canonicalizePath remaps any Icon\r variant in path to the canonical host
// name for the configured backend (e.g. Icon0x0D→Icon_ in legacy mode).
// This is applied during path resolution so both reads and writes use the
// correct on-disk name without duplicating the alias logic in every handler.
func (s *AFPService) canonicalizePath(path string) string {
	m := s.metaForPath(path)
	if m == nil {
		return path
	}
	base := filepath.Base(path)
	canonical := m.IconFileName()
	if isIconFile(base) && base != canonical {
		return filepath.Join(filepath.Dir(path), canonical)
	}
	return path
}

// alwaysHiddenNames lists directory and file names that are always hidden from
// AFP clients regardless of volume backend or AppleDouble mode. Names are
// matched case-insensitively.
var alwaysHiddenNames = []string{
	".appledesktop",
	".appledouble",
}

func (s *AFPService) isMetadataArtifact(name string, isDir bool, volID uint16) bool {
	if !isDir && strings.EqualFold(name, cnid.SQLiteFilename) {
		return true
	}
	for _, hidden := range alwaysHiddenNames {
		if strings.EqualFold(name, hidden) {
			return true
		}
	}
	if m := s.metaFor(volID); m != nil {
		return m.IsMetadataArtifact(name, isDir)
	}
	return strings.HasPrefix(name, "._")
}

// moveAppleDoubleSidecar renames an AppleDouble sidecar (._name) alongside a
// primary file rename/move. This is best-effort: missing sidecars are silently
// ignored, and unexpected errors are logged but not returned to the caller so
// that a sidecar failure never causes the already-completed primary operation
// to report an error to the client.
func (s *AFPService) moveAppleDoubleSidecar(oldPath, newPath string) error {
	m := s.metaForPath(oldPath)
	if m == nil {
		return nil
	}
	if err := m.MoveMetadata(oldPath, newPath); err != nil {
		log.Printf("[AFP] warning: could not move metadata %s → %s: %v", oldPath, newPath, err)
	}
	return nil
}

// deleteAppleDoubleSidecar removes a file's AppleDouble sidecar. This is
// best-effort: missing sidecars are silently ignored, and unexpected errors
// are logged but not returned to the caller.
func (s *AFPService) deleteAppleDoubleSidecar(path string) error {
	m := s.metaForPath(path)
	if m == nil {
		return nil
	}
	if err := m.DeleteMetadata(path); err != nil {
		log.Printf("[AFP] warning: could not delete metadata for %s: %v", path, err)
	}
	return nil
}

// calcVolParamsSize returns the total byte size of all fixed fields (including
// variable-name offset pointers) for a volume parameter block with the given bitmap.
func calcVolParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&VolBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&VolBitmapSignature != 0 {
		size += 2
	}
	if bitmap&VolBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&VolBitmapModDate != 0 {
		size += 4
	}
	if bitmap&VolBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&VolBitmapVolID != 0 {
		size += 2
	}
	if bitmap&VolBitmapBytesFree != 0 {
		size += 4
	}
	if bitmap&VolBitmapBytesTotal != 0 {
		size += 4
	}
	if bitmap&VolBitmapName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&VolBitmapExtBytesFree != 0 {
		size += 8
	}
	if bitmap&VolBitmapExtBytesTotal != 0 {
		size += 8
	}
	if bitmap&VolBitmapBlockSize != 0 {
		size += 4
	}
	return size
}

// calcDirParamsSize returns the total byte size of all fixed fields (including
// variable-name offset pointers) for a directory parameter block with the given bitmap.
func calcDirParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&DirBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&DirBitmapParentDID != 0 {
		size += 4
	}
	if bitmap&DirBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&DirBitmapModDate != 0 {
		size += 4
	}
	if bitmap&DirBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&DirBitmapFinderInfo != 0 {
		size += 32
	}
	if bitmap&DirBitmapLongName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&DirBitmapShortName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&DirBitmapDirID != 0 {
		size += 4
	}
	if bitmap&DirBitmapOffspringCount != 0 {
		size += 2
	}
	if bitmap&DirBitmapOwnerID != 0 {
		size += 4
	}
	if bitmap&DirBitmapGroupID != 0 {
		size += 4
	}
	if bitmap&DirBitmapAccessRights != 0 {
		size += 4
	}
	if bitmap&DirBitmapProDOSInfo != 0 {
		size += 6
	}
	return size
}

// calcFileParamsSize returns the total byte size of all fixed fields (including
// variable-name offset pointers) for a file parameter block with the given bitmap.
func calcFileParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&FileBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&FileBitmapParentDID != 0 {
		size += 4
	}
	if bitmap&FileBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&FileBitmapModDate != 0 {
		size += 4
	}
	if bitmap&FileBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&FileBitmapFinderInfo != 0 {
		size += 32
	}
	if bitmap&FileBitmapLongName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&FileBitmapShortName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&FileBitmapFileNum != 0 {
		size += 4
	}
	if bitmap&FileBitmapDataForkLen != 0 {
		size += 4
	}
	if bitmap&FileBitmapRsrcForkLen != 0 {
		size += 4
	}
	if bitmap&FileBitmapProDOSInfo != 0 {
		size += 6
	}
	return size
}

func (s *AFPService) packFileInfo(buf *bytes.Buffer, volumeID uint16, bitmap uint16, parentPath, name string, info fs.FileInfo, isDir bool) {
	var varBuf bytes.Buffer
	fullPath := filepath.Join(parentPath, name)
	name = s.catalogNameForPath(volumeID, fullPath, name)
	volFS := s.fsForVolume(volumeID)

	metadata := ForkMetadata{}
	if m := s.metaFor(volumeID); m != nil {
		if md, err := m.ReadForkMetadata(fullPath); err == nil {
			metadata = md
		}
	}
	if !isDir && !hasFinderTypeCreator(metadata.FinderInfo) && s.options.ExtensionMap != nil {
		if mapping, ok := s.options.ExtensionMap.Lookup(fullPath); ok {
			metadata.FinderInfo = applyExtensionMapping(metadata.FinderInfo, mapping)
		}
	}

	if isDir {
		fixedSize := calcDirParamsSize(bitmap)

		if bitmap&DirBitmapAttributes != 0 {
			var dirAttrs uint16
			if volFS != nil && volFS.Capabilities().DirAttributes {
				if attrs, err := volFS.DirAttributes(fullPath); err == nil {
					dirAttrs = attrs
				}
			}
			binary.Write(buf, binary.BigEndian, dirAttrs)
		}
		if bitmap&DirBitmapParentDID != 0 {
			// The root directory (DID=2) has a logical parent DID of 1.
			var pdir uint32
			thisDID := s.getPathDID(volumeID, fullPath)
			if thisDID == CNIDRoot {
				pdir = CNIDParentOfRoot
			} else {
				pdir = s.getPathDID(volumeID, parentPath)
			}
			binary.Write(buf, binary.BigEndian, pdir)
		}
		if bitmap&DirBitmapCreateDate != 0 {
			binary.Write(buf, binary.BigEndian, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&DirBitmapModDate != 0 {
			binary.Write(buf, binary.BigEndian, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&DirBitmapBackupDate != 0 {
			binary.Write(buf, binary.BigEndian, uint32(0))
		}
		if bitmap&DirBitmapFinderInfo != 0 {
			buf.Write(metadata.FinderInfo[:])
		}
		if bitmap&DirBitmapLongName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binary.Write(buf, binary.BigEndian, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&DirBitmapShortName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binary.Write(buf, binary.BigEndian, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&DirBitmapDirID != 0 {
			did := s.getPathDID(volumeID, fullPath)
			binary.Write(buf, binary.BigEndian, did)
		}
		if bitmap&DirBitmapOffspringCount != 0 {
			count := uint16(0)
			if volFS != nil && volFS.Capabilities().ChildCount {
				if cachedCount, err := volFS.ChildCount(fullPath); err == nil {
					count = cachedCount
				} else if entries, dirErr := volFS.ReadDir(fullPath); dirErr == nil {
					for _, e := range entries {
						if !s.isMetadataArtifact(e.Name(), e.IsDir(), volumeID) {
							count++
						}
					}
				}
			} else if volFS != nil {
				if entries, err := volFS.ReadDir(fullPath); err == nil {
					for _, e := range entries {
						if !s.isMetadataArtifact(e.Name(), e.IsDir(), volumeID) {
							count++
						}
					}
				}
			}
			binary.Write(buf, binary.BigEndian, count)
		}
		if bitmap&DirBitmapOwnerID != 0 {
			binary.Write(buf, binary.BigEndian, uint32(0))
		}
		if bitmap&DirBitmapGroupID != 0 {
			binary.Write(buf, binary.BigEndian, uint32(0))
		}
		if bitmap&DirBitmapAccessRights != 0 {
			rights := uint32(0x87070707)
			if s.volumeIsReadOnly(volumeID) {
				// Read-only volumes should advertise read+search rights, not write.
				rights = 0x87030303
			}
			binary.Write(buf, binary.BigEndian, rights)
		}
		if bitmap&DirBitmapProDOSInfo != 0 {
			buf.Write(make([]byte, 6))
		}
	} else {
		fixedSize := calcFileParamsSize(bitmap)

		if bitmap&FileBitmapAttributes != 0 {
			attr := uint16(0)
			if s.volumeIsReadOnly(volumeID) {
				attr |= FileAttrWriteInhibit
			}
			binary.Write(buf, binary.BigEndian, attr)
		}
		if bitmap&FileBitmapParentDID != 0 {
			pdir := s.getPathDID(volumeID, parentPath)
			binary.Write(buf, binary.BigEndian, pdir)
		}
		if bitmap&FileBitmapCreateDate != 0 {
			binary.Write(buf, binary.BigEndian, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&FileBitmapModDate != 0 {
			binary.Write(buf, binary.BigEndian, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&FileBitmapBackupDate != 0 {
			binary.Write(buf, binary.BigEndian, uint32(0))
		}
		if bitmap&FileBitmapFinderInfo != 0 {
			buf.Write(metadata.FinderInfo[:])
		}
		if bitmap&FileBitmapLongName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binary.Write(buf, binary.BigEndian, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&FileBitmapShortName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binary.Write(buf, binary.BigEndian, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&FileBitmapFileNum != 0 {
			did := s.getPathDID(volumeID, fullPath)
			binary.Write(buf, binary.BigEndian, did)
		}
		if bitmap&FileBitmapDataForkLen != 0 {
			binary.Write(buf, binary.BigEndian, uint32(info.Size()))
		}
		if bitmap&FileBitmapRsrcForkLen != 0 {
			binary.Write(buf, binary.BigEndian, uint32(metadata.ResourceForkLen))
		}
		if bitmap&FileBitmapProDOSInfo != 0 {
			buf.Write(make([]byte, 6))
		}
	}

	buf.Write(varBuf.Bytes())
}

func (s *AFPService) catalogNameForPath(volumeID uint16, fullPath, fallbackName string) string {
	cleanPath := filepath.Clean(fullPath)
	for i := range s.Volumes {
		vol := s.Volumes[i]
		if vol.ID != volumeID {
			continue
		}
		if cleanPath == filepath.Clean(vol.Config.Path) && vol.Config.Name != "" {
			return vol.Config.Name
		}
		break
	}
	return fallbackName
}

func toAFPTime(t time.Time) uint32 {
	epoch := time.Date(1904, 1, 1, 0, 0, 0, 0, time.Local)
	if t.Before(epoch) {
		return 0
	}
	secs := t.Sub(epoch).Seconds()
	if secs > float64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(secs)
}

func (s *AFPService) cnidStore(volumeID uint16) (CNIDStore, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	store, ok := s.cnidStores[volumeID]
	return store, ok
}

func (s *AFPService) getPathDID(volumeID uint16, path string) uint32 {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return CNIDInvalid
	}
	return store.Ensure(path)
}

func (s *AFPService) getDIDPath(volumeID uint16, did uint32) (string, bool) {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return "", false
	}
	return store.Path(did)
}

func (s *AFPService) resolveDIDPath(volumeID uint16, did uint32) (string, bool) {
	if did == CNIDInvalid {
		return "", false
	}
	return s.getDIDPath(volumeID, did)
}

func (s *AFPService) rebindDIDSubtree(volumeID uint16, oldPath, newPath string) {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return
	}
	store.Rebind(oldPath, newPath)
}

func (s *AFPService) removeDIDSubtree(volumeID uint16, path string) {
	store, ok := s.cnidStore(volumeID)
	if !ok {
		return
	}
	store.Remove(path)
}

func (s *AFPService) resolvePath(parentPath, name string, pathType uint8) (string, int32) {
	if pathType == 1 {
		// Short names are not supported.
		return "", ErrObjectNotFound
	}

	// AFP pathnames are separated by null bytes (\x00).
	// A single leading null byte is ignored.
	if len(name) > 0 && name[0] == '\x00' {
		name = name[1:]
	}

	// A pathname string is composed of CNode names separated by null bytes.
	// Consecutive null bytes ascend the directory tree:
	// Two consecutive null bytes ascend one level.
	// Three consecutive null bytes ascend two levels, etc.
	elements := strings.Split(name, "\x00")
	currentPath := parentPath

	for i := 0; i < len(elements); i++ {
		el := elements[i]
		if el == "" {
			// Empty element means a null byte following another null byte (or a leading/trailing one).
			// If it's the last element, it represents a trailing null byte which we can ignore.
			if i == len(elements)-1 {
				continue
			}
			// Each consecutive null byte (after the first separator) means ascending one level.
			// "To ascend one level... two consecutive null bytes should follow the offspring CNode name."
			// If we see an empty string here, it corresponds to ascending.
			currentPath = filepath.Dir(currentPath)
		} else {
			hostEl := s.afpPathElementToHost(el)
			if hostEl == ".." {
				return "", ErrAccessDenied
			}
			if !s.options.DecomposedFilenames && hasHostReservedChar(hostEl) {
				return "", ErrAccessDenied
			}
			currentPath = s.canonicalizePath(filepath.Join(currentPath, hostEl))
		}
	}

	fullPath := filepath.Clean(currentPath)

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(vol.Config.Path, fullPath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return fullPath, NoErr
		}
	}
	return "", ErrAccessDenied
}

func (s *AFPService) resolveSetPath(volumeID uint16, dirID uint32, path string, pathType uint8) (string, int32) {
	parentPath, ok := s.resolveDIDPath(volumeID, dirID)
	if !ok && dirID != 0 {
		return "", ErrObjectNotFound
	} else if !ok {
		parentPath, _ = s.resolveDIDPath(volumeID, CNIDRoot)
	}
	if path == "" {
		return parentPath, NoErr
	}
	return s.resolvePath(parentPath, path, pathType)
}

func (s *AFPService) applyFinderInfo(bitmap uint16, finderInfo [32]byte, targetPath string, volID uint16) {
	if bitmap&FileBitmapFinderInfo != 0 {
		m := s.metaFor(volID)
		if m == nil {
			return
		}
		if err := m.WriteFinderInfo(targetPath, finderInfo); err != nil {
			log.Printf("[AFP] writeFinderInfo %q: %v", targetPath, err)
		}
	}
}

func (s *AFPService) handleGetSrvrMsg(req *FPGetSrvrMsgReq) (*FPGetSrvrMsgRes, int32) {
	return &FPGetSrvrMsgRes{
		MessageType: req.MessageType,
		Bitmap:      0,
		Message:     "",
	}, NoErr
}
