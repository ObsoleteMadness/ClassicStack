package finder

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

// OpenVolume mounts a volume on an existing remote session (no-op for local).
func (s *Service) OpenVolume(sessionID, volume string) (*SessionInfo, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return nil, err
	}
	if sess.FS != nil && (sess.Volume == volume || volume == "") {
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
	if err := s.connectRemoteVolume(sess, volume); err != nil {
		return nil, err
	}
	return sess.info(), nil
}

func (sess *Session) info() *SessionInfo {
	var root uint32
	if sess.FS != nil {
		root = sess.FS.Meta().RootCNID()
		if root == 0 {
			root = sess.FS.Meta().EnsureCNID("")
		}
	}
	return &SessionInfo{
		SessionID:  sess.ID,
		ServerName: sess.ServerName,
		Kind:       sess.Kind,
		Volumes:    sess.Volumes,
		AllowGuest: sess.Kind == "local",
		RootID:     root,
	}
}

func (sess *Session) requireFS() (fs.ForkFS, error) {
	if sess.FS == nil {
		return nil, fmt.Errorf("finder: no volume open on session %s", sess.ID)
	}
	return sess.FS, nil
}

func nodeFrom(ffs fs.ForkFS, path string, name string, isDir bool, parentID uint32) (Node, error) {
	meta := ffs.Meta()
	id := meta.EnsureCNID(path)
	n := Node{
		ID:       id,
		ParentID: parentID,
		Name:     name,
		IsDir:    isDir,
	}
	if info, err := ffs.Stat(path); err == nil {
		n.ModDate = afpproto.MacTime(info.ModTime())
		n.CreateDate = n.ModDate
		if !isDir {
			n.DataBytes = info.Size()
		}
	}
	if fi, ok, err := ffs.ReadFinderInfo(path); err == nil && ok {
		n.FinderInfo = fi[:]
	} else {
		n.FinderInfo = make([]byte, 32)
	}
	if !isDir {
		if sz, err := ffs.ForkLen(path, fs.DataFork); err == nil {
			n.DataBytes = sz
		}
		if sz, err := ffs.ForkLen(path, fs.ResourceFork); err == nil {
			n.ResourceBytes = sz
		}
	}
	return n, nil
}
