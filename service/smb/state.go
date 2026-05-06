package smb

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

type connKey uint64

type connState struct {
	mu            sync.Mutex
	uid           uint16
	tids          map[uint16]treeSlot
	fids          map[uint16]*fileHandle
	searches      map[uint16]*searchHandle
	nextTID       uint16
	nextFID       uint16
	nextSID       uint16
	maxBufferSize uint16
}

type treeSlot struct {
	shareIdx int
}

type fileHandle struct {
	file     vfs.File
	path     string
	tid      uint16
	writable bool
}

type searchHandle struct {
	entries []fs.DirEntry
	idx     int
	tid     uint16
	pattern string
	attrs   uint16
}

type lockEntry struct {
	fid    uint16
	pid    uint16
	start  int64
	length int64
}

type lockTable struct {
	mu    sync.Mutex
	locks []lockEntry
}

func connKeyFromSession(ctx netbios.SessionContext) connKey {
	if ctx.SourceConnID != 0 {
		return connKey(ctx.SourceConnID)
	}
	h := fnv.New64a()
	_, _ = h.Write(ctx.Remote.Network[:])
	_, _ = h.Write(ctx.Remote.Node[:])
	_, _ = h.Write(ctx.Remote.Socket[:])
	return connKey(h.Sum64())
}

func (s *Service) allocUID() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextUID++
	if s.nextUID == 0 {
		s.nextUID++
	}
	return s.nextUID
}

func (s *Service) lookupConn(connID connKey) *connState {
	s.connsMu.Lock()
	conn := s.conns[connID]
	s.connsMu.Unlock()
	return conn
}

func (s *Service) ensureConn(connID connKey) *connState {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.conns == nil {
		s.conns = map[connKey]*connState{}
	}
	if conn := s.conns[connID]; conn != nil {
		return conn
	}
	conn := &connState{
		tids:     map[uint16]treeSlot{},
		fids:     map[uint16]*fileHandle{},
		searches: map[uint16]*searchHandle{},
	}
	s.conns[connID] = conn
	return conn
}

func (s *Service) dropConn(connID connKey) {
	s.connsMu.Lock()
	conn := s.conns[connID]
	delete(s.conns, connID)
	s.connsMu.Unlock()
	if conn != nil {
		s.closeConnFiles(conn)
	}
}

func (s *Service) closeConnFiles(conn *connState) {
	conn.mu.Lock()
	files := make([]*fileHandle, 0, len(conn.fids))
	for _, h := range conn.fids {
		if h != nil {
			files = append(files, h)
		}
	}
	conn.fids = map[uint16]*fileHandle{}
	conn.searches = map[uint16]*searchHandle{}
	conn.tids = map[uint16]treeSlot{}
	conn.mu.Unlock()
	for _, h := range files {
		_ = h.file.Close()
	}
}

func (s *Service) dropAllConnectionsLocked() {
	s.connsMu.Lock()
	all := make([]*connState, 0, len(s.conns))
	for _, conn := range s.conns {
		all = append(all, conn)
	}
	s.conns = map[connKey]*connState{}
	s.connsMu.Unlock()
	for _, conn := range all {
		s.closeConnFiles(conn)
	}
}

func (s *Service) initShareBackendsLocked() error {
	shareFSes := map[int]vfs.FileSystem{}
	shareNameToIndex := map[string]int{}

	for idx, share := range s.shares {
		name := normalizeBrowserName(share.Name)
		if name == "" {
			return fmt.Errorf("smb: share %d has empty name", idx)
		}
		if _, exists := shareNameToIndex[name]; exists {
			return fmt.Errorf("smb: duplicate share name %q", share.Name)
		}

		fsType := strings.TrimSpace(share.FSType)
		if fsType == "" {
			fsType = "local_fs"
		}
		fsys, err := vfs.New(fsType, vfs.Params{
			Name:     share.Name,
			Path:     share.Path,
			ReadOnly: share.ReadOnly,
		})
		if err != nil {
			return fmt.Errorf("smb: init share %q (%s): %w", share.Name, fsType, err)
		}

		shareFSes[idx] = fsys
		shareNameToIndex[name] = idx
	}

	s.shareFSes = shareFSes
	s.shareNameToIndex = shareNameToIndex
	return nil
}
