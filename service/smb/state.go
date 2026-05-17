package smb

import (
	"fmt"
	"hash/fnv"
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
	lockTables    map[string]*lockTable
	nextTID  uint16
	nextFID  uint16
	nextSID  uint16
}

type treeSlot struct {
	shareIdx int
}

type fileHandle struct {
	file     vfs.File
	path     string
	writable bool
	offset   int64
	// mpxAccum is the running OR of RequestMask values from every
	// SMB_COM_WRITE_MPX received since the last sequenced (final)
	// request. Per [MS-CIFS] 2.2.4.26.2 / 3.3.5.27 the server replies
	// only to the sequenced request (SMB header SequenceNumber != 0)
	// and returns this accumulated mask as ResponseMask. Replying to
	// non-sequenced requests breaks Win9x's window state machine and
	// causes it to skip chunks; staying silent until the sequencing
	// signal arrives is what the spec mandates and what works.
	mpxAccum uint32
}

type searchHandle struct {
	entries []findFirst2Row
	idx     int
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
		tids:       map[uint16]treeSlot{},
		fids:       map[uint16]*fileHandle{},
		searches:   map[uint16]*searchHandle{},
		lockTables: map[string]*lockTable{},
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
	conn.lockTables = map[string]*lockTable{}
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
			Name:            share.Name,
			Path:            share.Path,
			ReadOnly:        share.ReadOnly,
			ShortnameMapper: s.opts.Shortname,
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
