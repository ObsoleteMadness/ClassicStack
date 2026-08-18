package finder

import (
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func toBusVolumes(vols []VolumeInfo) []bus.FinderVolume {
	out := make([]bus.FinderVolume, len(vols))
	for i, v := range vols {
		out[i] = bus.FinderVolume{
			ID:        v.ID,
			Kind:      v.Kind,
			Title:     v.Title,
			Subtitle:  v.Subtitle,
			Protocol:  v.Protocol,
			Transport: v.Transport,
			Address:   v.Address,
			URI:       v.URI,
			OS:        v.OS,
			Version:   v.Version,
			ReadOnly:  v.ReadOnly,
		}
	}
	return out
}

func volumeListsEqual(a, b []VolumeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

func (s *Service) publishFinder(ev bus.FinderUpdated) {
	if s.pub == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	s.log.Log(log.Debug, "finder publish",
		log.Str("kind", ev.Kind), log.Str("scheme", ev.Scheme),
		log.Bool("scanning", ev.Scanning), log.Int("count", int64(len(ev.Volumes))))
	s.pub.Publish(ev)
}

func (s *Service) publishNetworks(scheme string, vols []VolumeInfo) {
	s.publishFinder(bus.FinderUpdated{
		Kind:    bus.FinderKindNetworks,
		Scheme:  scheme,
		Volumes: toBusVolumes(vols),
	})
}

func (s *Service) publishScanning(scanning bool) {
	s.publishFinder(bus.FinderUpdated{
		Kind:     bus.FinderKindScanning,
		Scanning: scanning,
	})
}
