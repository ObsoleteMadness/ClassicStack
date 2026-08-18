package finder

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	stdfs "io/fs"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	afpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

func newSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// OpenLocal binds an operator session to a live share on this instance.
func (s *Service) OpenLocal(id string) (*SessionInfo, error) {
	proto, name, ok := parseLocalID(id)
	if !ok {
		return nil, fmt.Errorf("finder: invalid local id %q", id)
	}
	if existing := s.existingLocal(id, name); existing != nil {
		return existing.info(), nil
	}
	ffs, err := s.resolveLocalFS(proto, name)
	if err != nil {
		return nil, err
	}
	root := ffs.Meta().EnsureCNID("")
	sess := &Session{
		ID:         newSessionID(),
		Kind:       "local",
		ServerName: "ClassicStack",
		Volumes:    []string{name},
		Volume:     name,
		FS:         ffs,
		local:      true,
		readOnly:   ffs.Capabilities().ReadOnly,
		remoteURI:  id,
		allowGuest: true,
		touched:    time.Now(),
	}
	s.put(sess)
	s.log.Log2(log.Debug, "finder opened local volume",
		log.Str("session", sess.ID), log.Str("volume", name))
	return &SessionInfo{
		SessionID:  sess.ID,
		ServerName: sess.ServerName,
		Kind:       sess.Kind,
		Volumes:    sess.Volumes,
		AllowGuest: true,
		RootID:     root,
	}, nil
}

func (s *Service) existingLocal(id, name string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sess {
		if !sess.local || sess.FS == nil {
			continue
		}
		if sess.remoteURI == id || (sess.remoteURI == "" && sess.Volume == name) {
			sess.touch()
			return sess
		}
	}
	return nil
}

// OpenVolume mounts a volume on an existing remote session (no-op for local).
// A session holds one open volume. Opening a second volume from the same login
// clones the session so an already-mounted share keeps its catalog.
func (s *Service) OpenVolume(sessionID, volume string) (*SessionInfo, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return nil, err
	}
	if sess.FS != nil && (volume == "" || strings.EqualFold(sess.Volume, volume)) {
		return sess.info(), nil
	}
	if sess.local {
		return sess.info(), nil
	}
	if volume == "" {
		if len(sess.Volumes) == 1 {
			volume = sess.Volumes[0]
		} else {
			return nil, fmt.Errorf("finder: volume name required")
		}
	}
	if found := s.existingVolume(sess, volume); found != nil {
		return found.info(), nil
	}
	if sess.FS != nil && !strings.EqualFold(sess.Volume, volume) {
		clone := cloneLogin(sess)
		if err := s.connectRemoteVolume(clone, volume); err != nil {
			return nil, err
		}
		s.put(clone)
		s.log.Log(log.Debug, "finder cloned session for volume",
			log.Str("from", sessionID), log.Str("session", clone.ID), log.Str("volume", volume))
		return clone.info(), nil
	}
	if err := s.connectRemoteVolume(sess, volume); err != nil {
		return nil, err
	}
	return sess.info(), nil
}

func cloneLogin(sess *Session) *Session {
	vols := append([]string(nil), sess.Volumes...)
	return &Session{
		ID:         newSessionID(),
		Kind:       sess.Kind,
		ServerName: sess.ServerName,
		Volumes:    vols,
		remoteURI:  sess.remoteURI,
		remoteUser: sess.remoteUser,
		remotePass: sess.remotePass,
		ifaceType:  sess.ifaceType,
		iface:      sess.iface,
		transport:  sess.transport,
		os:         sess.os,
		dialect:    sess.dialect,
		uams:       append([]string(nil), sess.uams...),
		allowGuest: sess.allowGuest,
		touched:    time.Now(),
	}
}

func (sess *Session) info() *SessionInfo {
	var root uint32
	if sess.FS != nil {
		root = sess.FS.Meta().RootCNID()
		if root == 0 {
			root = sess.FS.Meta().EnsureCNID("")
		}
	}
	vols := sess.Volumes
	if sess.Volume != "" && len(vols) == 0 {
		vols = []string{sess.Volume}
	}
	return &SessionInfo{
		SessionID:  sess.ID,
		ServerName: sess.ServerName,
		Kind:       sess.Kind,
		Volumes:    vols,
		AllowGuest: sess.allowGuest || sess.Kind == "local",
		UAMs:       append([]string(nil), sess.uams...),
		RootID:     root,
		Volume:     sess.Volume,
		Target:     sess.remoteURI,
		Transport:  sess.transport,
		OS:         sess.os,
		Dialect:    sess.dialect,
	}
}

// CloseVolume releases one opened volume (browse ForkFS and matching host mount)
// while keeping the login session so other volumes can still be opened.
func (s *Service) CloseVolume(sessionID, volume string) error {
	volume = strings.TrimSpace(volume)
	sess, err := s.get(sessionID)
	if err != nil {
		return err
	}
	if sess.local {
		return nil
	}
	if volume == "" {
		volume = sess.Volume
	}
	if volume == "" {
		return fmt.Errorf("finder: volume name required")
	}

	s.mu.Lock()
	var hostIDs []string
	for id, m := range s.mounts {
		if m == nil {
			continue
		}
		if !strings.EqualFold(m.info.Volume, volume) {
			continue
		}
		if m.info.Server != "" &&
			!strings.EqualFold(m.info.Server, sess.ServerName) &&
			!strings.EqualFold(m.info.Server, sess.remoteURI) {
			continue
		}
		hostIDs = append(hostIDs, id)
	}
	s.mu.Unlock()
	for _, id := range hostIDs {
		if err := s.Unmount(id); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sess[sessionID]
	if !ok {
		return nil
	}
	if sess.hostMount {
		return nil
	}
	if sess.FS != nil && (sess.Volume == "" || strings.EqualFold(sess.Volume, volume)) {
		sess.closeLocked()
		sess.Volume = ""
		s.log.Log2(log.Debug, "finder ejected volume",
			log.Str("session", sessionID), log.Str("volume", volume))
	}
	return nil
}

func (sess *Session) requireFS() (fs.ForkFS, error) {
	if sess.FS == nil {
		return nil, fmt.Errorf("finder: no volume open on session %s", sess.ID)
	}
	return sess.FS, nil
}

func nodeFrom(ffs fs.ForkFS, path string, name string, isDir bool, parentID uint32) (Node, error) {
	n := newNode(ffs, path, name, isDir, parentID)
	info, err := ffs.Stat(path)
	if err == nil {
		hasFinder, hasRsrc := applyFileInfo(&n, info)
		if hasFinder && (isDir || hasRsrc) {
			// AFP Enumerate/GetFileDirParms already carried Finder info and fork
			// lengths; do not issue per-child FPGetFileDirParms / FPGetVolParms.
			return n, nil
		}
		if hasFinder && !isDir {
			if !hasRsrc {
				if sz, err := ffs.ForkLen(path, fs.ResourceFork); err == nil {
					n.ResourceBytes = sz
				}
			}
			return n, nil
		}
	}
	if fi, ok, err := ffs.ReadFinderInfo(path); err == nil && ok {
		n.FinderInfo = fi[:]
	}
	if !isDir {
		if n.DataBytes == 0 {
			if sz, err := ffs.ForkLen(path, fs.DataFork); err == nil {
				n.DataBytes = sz
			}
		}
		if sz, err := ffs.ForkLen(path, fs.ResourceFork); err == nil {
			n.ResourceBytes = sz
		}
	}
	return n, nil
}

func nodeFromEntry(ffs fs.ForkFS, path string, e stdfs.DirEntry, parentID uint32) (Node, error) {
	info, err := e.Info()
	if err != nil {
		return nodeFrom(ffs, path, e.Name(), e.IsDir(), parentID)
	}
	n := newNode(ffs, path, e.Name(), e.IsDir(), parentID)
	hasFinder, hasRsrc := applyFileInfo(&n, info)
	if hasFinder && (e.IsDir() || hasRsrc) {
		return n, nil
	}
	if hasFinder && !e.IsDir() {
		if !hasRsrc {
			if sz, err := ffs.ForkLen(path, fs.ResourceFork); err == nil {
				n.ResourceBytes = sz
			}
		}
		return n, nil
	}
	return nodeFrom(ffs, path, e.Name(), e.IsDir(), parentID)
}

func newNode(ffs fs.ForkFS, path, name string, isDir bool, parentID uint32) Node {
	return Node{
		ID:         ffs.Meta().EnsureCNID(path),
		ParentID:   parentID,
		Name:       name,
		IsDir:      isDir,
		FinderInfo: make([]byte, 32),
	}
}

// applyFileInfo copies Stat/DirEntry metadata onto n. hasFinder/hasRsrc report
// whether FileInfo.Sys() already carried those fields from the wire (AFP
// enumerate), so the caller can skip extra fork/Finder-info round-trips.
func applyFileInfo(n *Node, info stdfs.FileInfo) (hasFinder, hasRsrc bool) {
	n.ModDate = afpproto.MacTime(info.ModTime())
	n.CreateDate = n.ModDate
	if sys := info.Sys(); sys != nil {
		if ct, ok := sys.(fs.DOSCreateTimeInfo); ok {
			if t := ct.DOSCreateTime(); !t.IsZero() {
				n.CreateDate = afpproto.MacTime(t)
			}
		}
		if fi, ok := sys.(fs.FinderInfoBits); ok {
			if bits, present := fi.FinderInfo(); present {
				b := bits
				n.FinderInfo = b[:]
				hasFinder = true
			}
		}
		if rl, ok := sys.(fs.ResourceLenInfo); ok {
			n.ResourceBytes = rl.ResourceForkLen()
			hasRsrc = true
		}
	}
	if !n.IsDir {
		n.DataBytes = info.Size()
	}
	return hasFinder, hasRsrc
}
