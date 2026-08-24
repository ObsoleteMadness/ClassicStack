package finder

import (
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/browser"
	"github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// LocalVolumes lists this instance’s bound AFP/SMB/NCP/EtherDFS shares. Always
// non-nil (encodes as JSON "[]" rather than "null") since GET /finder/local
// feeds the web UI's sidebar merge, which spreads the result as an array.
func (s *Service) LocalVolumes() []VolumeInfo {
	out := []VolumeInfo{}
	if s.src == nil {
		return out
	}
	if svc := s.afp(); svc != nil {
		for _, v := range svc.Volumes() {
			out = append(out, VolumeInfo{
				ID:       localID(KindAFP, v.Name()),
				Kind:     KindLocal,
				Title:    v.Name(),
				Subtitle: "AFP",
				Protocol: KindAFP,
				ReadOnly: v.FS().Capabilities().ReadOnly,
			})
		}
	}
	if svc := s.smb(); svc != nil {
		for _, info := range svc.Shares() {
			out = append(out, VolumeInfo{
				ID:       localID(KindSMB, info.Name),
				Kind:     KindLocal,
				Title:    info.Name,
				Subtitle: "SMB",
				Protocol: KindSMB,
				ReadOnly: info.ReadOnly,
			})
		}
	}
	if svc := s.ncp(); svc != nil {
		for _, info := range svc.Shares() {
			out = append(out, VolumeInfo{
				ID:       localID(KindNCP, info.Name),
				Kind:     KindLocal,
				Title:    info.Name,
				Subtitle: "NCP",
				Protocol: KindNCP,
				ReadOnly: info.ReadOnly,
			})
		}
	}
	if svc := s.etherdfs(); svc != nil {
		for _, d := range svc.BoundDrives() {
			out = append(out, VolumeInfo{
				ID:       localID(KindEtherDFS, d.Name()),
				Kind:     KindLocal,
				Title:    d.Name(),
				Subtitle: "EtherDFS",
				Protocol: KindEtherDFS,
				ReadOnly: d.ReadOnly(),
			})
		}
	}
	s.log.Log1(log.Debug, "finder listed local volumes", log.Int("count", int64(len(out))))
	return out
}

func localID(proto, name string) string {
	return "local:" + proto + ":" + name
}

func parseLocalID(id string) (proto, name string, ok bool) {
	if !strings.HasPrefix(id, "local:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, "local:")
	proto, name, ok = strings.Cut(rest, ":")
	return proto, name, ok && proto != "" && name != ""
}

func (s *Service) resolveLocalFS(proto, name string) (fs.ForkFS, error) {
	switch strings.ToLower(proto) {
	case KindAFP:
		svc := s.afp()
		if svc == nil {
			return nil, fmt.Errorf("finder: AFP service is not running")
		}
		for _, v := range svc.Volumes() {
			if v.Name() == name {
				return v.FS(), nil
			}
		}
		return nil, fmt.Errorf("finder: AFP volume %q not found: %w", name, ErrNotFound)
	case KindSMB:
		svc := s.smb()
		if svc == nil {
			return nil, fmt.Errorf("finder: SMB service is not running")
		}
		sh, ok := svc.ShareByName(name)
		if !ok {
			return nil, fmt.Errorf("finder: SMB share %q not found: %w", name, ErrNotFound)
		}
		return sh.FS(), nil
	case KindNCP:
		svc := s.ncp()
		if svc == nil {
			return nil, fmt.Errorf("finder: NCP service is not running")
		}
		v, ok := svc.VolumeByName(name)
		if !ok {
			return nil, fmt.Errorf("finder: NCP volume %q not found: %w", name, ErrNotFound)
		}
		return v.FS(), nil
	case KindEtherDFS:
		svc := s.etherdfs()
		if svc == nil {
			return nil, fmt.Errorf("finder: EtherDFS service is not running")
		}
		d, ok := svc.DriveByName(name)
		if !ok {
			return nil, fmt.Errorf("finder: EtherDFS drive %q not found: %w", name, ErrNotFound)
		}
		return d.FS(), nil
	default:
		return nil, fmt.Errorf("finder: unknown local protocol %q", proto)
	}
}

func (s *Service) afp() *afp.Service {
	if s.src == nil {
		return nil
	}
	c := s.src.Component(afp.Name)
	if c == nil {
		return nil
	}
	v, _ := c.(*afp.Service)
	return v
}

func (s *Service) smb() *smb.Service {
	if s.src == nil {
		return nil
	}
	c := s.src.Component(smb.Name)
	if c == nil {
		return nil
	}
	v, _ := c.(*smb.Service)
	return v
}

func (s *Service) ncp() *ncp.Service {
	if s.src == nil {
		return nil
	}
	c := s.src.Component(ncp.Name)
	if c == nil {
		return nil
	}
	v, _ := c.(*ncp.Service)
	return v
}

func (s *Service) etherdfs() *etherdfs.Service {
	if s.src == nil {
		return nil
	}
	c := s.src.Component(etherdfs.Name)
	if c == nil {
		return nil
	}
	v, _ := c.(*etherdfs.Service)
	return v
}

// browser returns the live NetBIOS browser service, or nil when this build has no
// Browser component (no `browser`/`all` tag) or the runtime has not built one. Used
// by the outbound NBIPX/NBF client (link.go) to share the server's already-observed
// browse list instead of running its own independent discovery.
func (s *Service) browser() *browser.Service {
	if s.src == nil {
		return nil
	}
	c := s.src.Component(browser.Name)
	if c == nil {
		return nil
	}
	v, _ := c.(*browser.Service)
	return v
}
