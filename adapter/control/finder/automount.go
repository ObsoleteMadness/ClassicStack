package finder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// autoMountRetry is the pause between failed auto-mount connect attempts.
const autoMountRetry = 2 * time.Second

func (s *Service) fuseConfig() config.FUSESection {
	m := s.model()
	if m == nil {
		return config.DefaultFUSE()
	}
	return m.FUSE
}

func (s *Service) alreadyMountedAt(point string) bool {
	point = strings.TrimSpace(point)
	if point == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.mounts {
		if m.info.Mountpoint == point {
			return true
		}
	}
	return false
}

// autoMountAll attaches each configured [[fusevolumes]] share, retrying connect
// failures until the FUSE mount timeout (or ctx) expires.
func (s *Service) autoMountAll(ctx context.Context) {
	if !s.clientEnabled() {
		return
	}
	if !s.mountAllowed() {
		s.log.Log0(log.Debug, "fuse auto-mount skipped ([Client].mount is off)")
		return
	}
	if !platformMountAvailable() {
		s.log.Log0(log.Debug, "fuse auto-mount skipped (host mount unavailable)")
		return
	}
	vols := config.FUSEVolumesFromModel(s.model())
	if len(vols) == 0 {
		return
	}
	timeout := s.fuseConfig().MountTimeout()
	s.log.Log(log.Info, "fuse auto-mount starting",
		log.Int("count", int64(len(vols))),
		log.Int("timeout_seconds", int64(timeout/time.Second)))
	for _, vol := range vols {
		if ctx.Err() != nil {
			s.log.Log0(log.Debug, "fuse auto-mount cancelled")
			return
		}
		s.autoMountOne(ctx, vol, timeout)
	}
}

func (s *Service) autoMountOne(ctx context.Context, vol *config.FUSEVolumeSection, timeout time.Duration) {
	req, err := mountRequestFromVolume(vol)
	if err != nil {
		s.log.Log2(log.Error, "fuse auto-mount skipped",
			log.Str("remote", vol.Remote), log.Str("err", err.Error()))
		return
	}
	if s.alreadyMountedAt(req.Mountpoint) {
		s.log.Log1(log.Debug, "fuse auto-mount already mounted", log.Str("mountpoint", req.Mountpoint))
		return
	}
	mountCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	attempt := 0
	for {
		attempt++
		if mountCtx.Err() != nil {
			break
		}
		s.log.Log(log.Debug, "fuse auto-mount connect",
			log.Str("remote", req.Target),
			log.Str("mountpoint", req.Mountpoint),
			log.Int("attempt", int64(attempt)))
		info, err := s.Mount(mountCtx, req)
		if err == nil {
			s.log.Log(log.Info, "fuse auto-mounted",
				log.Str("id", info.ID),
				log.Str("mountpoint", info.Mountpoint),
				log.Str("volume", info.Volume))
			return
		}
		lastErr = err
		if !retryableAutoMount(err) {
			s.log.Log2(log.Error, "fuse auto-mount failed",
				log.Str("mountpoint", req.Mountpoint), log.Str("err", err.Error()))
			return
		}
		s.log.Log(log.Debug, "fuse auto-mount retry",
			log.Str("mountpoint", req.Mountpoint),
			log.Str("err", err.Error()))
		select {
		case <-mountCtx.Done():
			lastErr = mountCtx.Err()
		case <-time.After(autoMountRetry):
			continue
		}
		break
	}
	if lastErr == nil {
		lastErr = mountCtx.Err()
	}
	s.log.Log(log.Error, "fuse auto-mount timed out",
		log.Str("mountpoint", req.Mountpoint),
		log.Str("remote", req.Target),
		log.Str("err", lastErr.Error()))
}

func retryableAutoMount(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMountUnavailable) || errors.Is(err, ErrMountDisabled) ||
		errors.Is(err, ErrClientDisabled) || errors.Is(err, ErrLocalMount) ||
		errors.Is(err, ErrServiceDisabled) {
		return false
	}
	return true
}

// mountRequestFromVolume maps a configured auto-mount volume to a host MountRequest.
func mountRequestFromVolume(vol *config.FUSEVolumeSection) (MountRequest, error) {
	if vol == nil {
		return MountRequest{}, fmt.Errorf("finder: nil fuse volume")
	}
	if err := vol.Validate(); err != nil {
		return MountRequest{}, err
	}
	target, err := uri.Parse(strings.TrimSpace(vol.Remote))
	if err != nil {
		return MountRequest{}, fmt.Errorf("finder: fuse remote: %w", err)
	}
	if strings.TrimSpace(target.Volume) == "" {
		return MountRequest{}, fmt.Errorf("finder: fuse remote %q has no volume", vol.Remote)
	}
	return MountRequest{
		Kind:       target.Scheme,
		Target:     strings.TrimSpace(vol.Remote),
		Volume:     target.Volume,
		User:       target.User,
		Password:   target.Pass,
		Guest:      !target.HasCreds,
		ReadOnly:   vol.ReadOnly,
		Mountpoint: strings.TrimSpace(vol.Mountpoint),
		IfaceType:  target.Transport,
	}, nil
}
