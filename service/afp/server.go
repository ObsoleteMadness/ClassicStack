//go:build afp

/*
Package afp implements the AppleTalk Filing Protocol (AFP) 2.x.

AFP is an application-layer protocol that allows users to share files and network
resources.

Inside Macintosh: Networking, Chapter 9.
https://dev.os9.ca/techpubs/mac/Networking/Networking-223.html
*/
package afp

import (
	"errors"
	"fmt"
	"hash/crc32"
	"log"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/protocol/ddp"
	"github.com/pgodw/omnitalk/service"
)

// Service implements AppleTalk Filing Protocol.
type Service struct {
	ServerName  string
	Volumes     []Volume
	fs          FileSystem
	volumeFS    map[uint16]FileSystem
	meta        ForkMetadataBackend            // global override when ForkMetadataBackend is injected via options
	metas       map[uint16]ForkMetadataBackend // per-volume backends (keyed by Volume.ID)
	mu          sync.RWMutex
	options     Options
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

	stop chan struct{}
	wg   sync.WaitGroup
}

func (s *Service) SetPacketDumper(dumper service.PacketDumper) {
	s.dumper = dumper
}

// SetMaxReadSize caps FPRead ReqCount to n bytes and propagates the same limit
// to any filesystem that supports range limiting (e.g. MacGardenFileSystem).
// ASP calls this with its quantum size so HTTP range requests from virtual
// filesystems never exceed what one ASP reply can carry. DSI leaves it at 0.
func (s *Service) SetMaxReadSize(n int) {
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

func (s *Service) logPacket(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if s.dumper != nil {
		s.dumper.LogPacket(msg)
	}
}

func NewService(serverName string, configs []VolumeConfig, fs FileSystem, transports []Transport, opts ...Options) *Service {
	options := DefaultOptions()
	if len(opts) > 0 {
		options = opts[0]
	}

	s := &Service{
		ServerName:  serverName,
		fs:          fs,
		stop:        make(chan struct{}),
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
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.rebuildDesktopDBsIfConfigured()
	}()
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
func (s *Service) metaFor(volID uint16) ForkMetadataBackend {
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
func (s *Service) metaForPath(path string) ForkMetadataBackend {
	clean := filepath.Clean(path)
	for _, vol := range s.Volumes {
		rel, err := filepath.Rel(vol.Config.Path, clean)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return s.metaFor(vol.ID)
		}
	}
	return s.meta
}

func (s *Service) fsForVolume(volID uint16) FileSystem {
	if fs, ok := s.volumeFS[volID]; ok && fs != nil {
		return fs
	}
	return s.fs
}

func (s *Service) fsForPath(path string) FileSystem {
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
	cfg.FSType = fsType
	cfg.Path = filepath.Clean(cfg.Path)
	return NewFS(cfg)
}

// Start initializes all underlying transports.
func (s *Service) Start(router service.Router) error {
	for _, t := range s.transports {
		if err := t.Start(router); err != nil {
			return err
		}
	}
	return nil
}

// Stop shuts down all underlying transports.
func (s *Service) Stop() error {
	var errs []error
	if s.stop != nil {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	for _, t := range s.transports {
		if err := t.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	s.wg.Wait()
	type closer interface{ Close() error }
	for _, fsys := range s.volumeFS {
		if c, ok := fsys.(closer); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("afp: stop: %w", errors.Join(errs...))
	}
	return nil
}

// Socket returns the AppleTalk socket number if any of the transports listen on one.
// We return asp.ServerSocket (252) if we have a transport that needs it.
func (s *Service) Socket() uint8 {
	// The router expects services that listen on a specific socket to return it here.
	// Since AFPService wraps transports, we return the well-known ASP socket (252).
	// TCP-only instances won't be called for AppleTalk routing anyway if they don't register NBP.
	return 252 // asp.ServerSocket
}

// Inbound delegates inbound DDP packets to the underlying transports.
func (s *Service) Inbound(d ddp.Datagram, p port.Port) {
	for _, t := range s.transports {
		t.Inbound(d, p)
	}
}

// GetStatus implements the CommandHandler interface
func (s *Service) GetStatus() []byte {
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

func (s *Service) HandleCommand(data []byte) (resBytes []byte, errCode int32) {
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

	spec, ok := commandRegistry[cmd]
	if !ok {
		log.Printf("[AFP] unknown command %d", cmd)
		return nil, ErrCallNotSupported
	}

	req := spec.newReq()
	cmdData := data
	if spec.stripCmdByte {
		cmdData = data[1:]
	}

	if err := req.Unmarshal(cmdData); err != nil {
		log.Printf("[AFP] Error unmarshaling cmd %d: %v", cmd, err)
		return nil, ErrParamErr
	}

	s.logPacket("[AFP] → %s", req.String())
	s.logResolvedPaths(req)

	res, errCode := spec.handle(s, req)

	if res != nil {
		s.logPacket("[AFP] ← %s (err=%d)", res.String(), errCode)
		resBytes = res.Marshal()
	} else if errCode != NoErr {
		s.logPacket("[AFP] ← cmd=%d err=%d", cmd, errCode)
	}

	return resBytes, errCode
}

func (s *Service) logResolvedPaths(req Request) {
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

func (s *Service) logResolvedPath(op string, volumeID uint16, dirID uint32, pathType uint8, rawPath string) {
	resolved, errCode := s.resolveVolumePath(volumeID, dirID, rawPath, pathType)
	if errCode == NoErr {
		log.Printf("[AFP][Path] %s vol=%d dirID=%d pathType=%d raw=%q resolved=%q", op, volumeID, dirID, pathType, rawPath, resolved)
		return
	}
	log.Printf("[AFP][Path] %s vol=%d dirID=%d pathType=%d raw=%q unresolved err=%d", op, volumeID, dirID, pathType, rawPath, errCode)
}

func (s *Service) logResolvedPathFromDTRef(op string, dtRefNum uint16, dirID uint32, pathType uint8, rawPath string) {
	s.mu.RLock()
	volID, ok := s.dtRefs[dtRefNum]
	s.mu.RUnlock()
	if !ok {
		log.Printf("[AFP][Path] %s dtRef=%d dirID=%d pathType=%d raw=%q unresolved err=%d", op, dtRefNum, dirID, pathType, rawPath, ErrParamErr)
		return
	}
	s.logResolvedPath(op, volID, dirID, pathType, rawPath)
}

func (s *Service) handleGetSrvrMsg(req *FPGetSrvrMsgReq) (*FPGetSrvrMsgRes, int32) {
	return &FPGetSrvrMsgRes{
		MessageType: req.MessageType,
		Bitmap:      0,
		Message:     "",
	}, NoErr
}
