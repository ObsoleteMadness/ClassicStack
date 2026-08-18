package finder

import (
	"sort"
	"strings"
	"time"
)

// MountedVolumes is GET /finder/mounted: every volume this process currently has
// open as a client. Host FUSE/WinFsp mounts and Finder browse sessions share one
// list so any web client sees the same mounts.
func (s *Service) MountedVolumes() []MountedVolume {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MountedVolume, 0, len(s.sess)+len(s.mounts))
	seen := map[string]bool{}
	for _, m := range s.mounts {
		sess := s.ensureBrowseForMountLocked(m)
		out = append(out, mountedFromSession(sess, m.info.Mountpoint))
		seen[sess.ID] = true
	}
	for _, sess := range s.sess {
		if sess.local || sess.FS == nil || seen[sess.ID] {
			continue
		}
		out = append(out, mountedFromSession(sess, ""))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServerName != out[j].ServerName {
			return out[i].ServerName < out[j].ServerName
		}
		if out[i].Volume != out[j].Volume {
			return out[i].Volume < out[j].Volume
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}

func mountedFromSession(sess *Session, mountpoint string) MountedVolume {
	info := sess.info()
	vol := info.Volume
	if vol == "" && len(info.Volumes) == 1 {
		vol = info.Volumes[0]
	}
	return MountedVolume{
		SessionID:  info.SessionID,
		Kind:       info.Kind,
		ServerName: info.ServerName,
		Volume:     vol,
		Target:     info.Target,
		Transport:  info.Transport,
		RootID:     info.RootID,
		Mountpoint: mountpoint,
	}
}

// ensureBrowseForMountLocked attaches a Finder session to a host mount so the
// HTTP catalog can browse it. The FUSE adapter owns the ForkFS.
func (s *Service) ensureBrowseForMountLocked(m *liveMount) *Session {
	if sess := s.sess[m.info.ID]; sess != nil && sess.FS == m.fsys {
		return sess
	}
	for _, sess := range s.sess {
		if sess.FS == m.fsys {
			return sess
		}
	}
	sess := &Session{
		ID:         m.info.ID,
		Kind:       m.info.Kind,
		ServerName: m.info.Server,
		Volumes:    []string{m.info.Volume},
		Volume:     m.info.Volume,
		FS:         m.fsys,
		hostMount:  true,
		remoteURI:  m.info.Server,
		touched:    time.Now(),
	}
	if sess.Kind == "" {
		sess.Kind = KindAFP
	}
	s.sess[sess.ID] = sess
	return sess
}

func (s *Service) existingMounted(kind, target string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matchMounted(kind, target, "")
}

func (s *Service) existingVolume(from *Session, volume string) *Session {
	if from == nil {
		return nil
	}
	target := strings.TrimSpace(from.remoteURI)
	if target == "" {
		target = strings.TrimSpace(from.ServerName)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := s.matchMounted(from.Kind, target, volume)
	if found != nil {
		found.touch()
	}
	return found
}

func (s *Service) matchMounted(kind, target, volume string) *Session {
	kind = strings.ToLower(strings.TrimSpace(kind))
	target = strings.TrimSpace(target)
	volume = strings.TrimSpace(volume)
	if target == "" {
		return nil
	}
	var bestLogin, bestVol *Session
	for _, sess := range s.sess {
		if sess.local {
			continue
		}
		if kind != "" && !strings.EqualFold(sess.Kind, kind) {
			continue
		}
		if !sameMountTarget(sess, target) {
			continue
		}
		if volume != "" {
			if sess.FS == nil || sess.Volume == "" || !strings.EqualFold(sess.Volume, volume) {
				continue
			}
			if bestVol == nil || sess.ID < bestVol.ID {
				bestVol = sess
			}
			continue
		}
		if sess.FS == nil {
			if bestLogin == nil || sess.ID < bestLogin.ID {
				bestLogin = sess
			}
			continue
		}
		if bestVol == nil || sess.ID < bestVol.ID {
			bestVol = sess
		}
	}
	if bestLogin != nil {
		return bestLogin
	}
	return bestVol
}

func sameMountTarget(sess *Session, target string) bool {
	t := strings.TrimSpace(target)
	if t == "" {
		return false
	}
	if strings.EqualFold(sess.ID, t) {
		return true
	}
	if strings.HasPrefix(strings.ToLower(t), "mounted:") && strings.EqualFold(sess.ID, t[len("mounted:"):]) {
		return true
	}
	if strings.EqualFold(sess.remoteURI, t) {
		return true
	}
	// Bare server name (not a URI) may match the AFP/SMB display name.
	if !strings.Contains(t, "://") && strings.EqualFold(sess.ServerName, t) {
		return true
	}
	return false
}
