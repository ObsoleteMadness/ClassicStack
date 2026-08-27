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

// volumeListsEqual compares by ID as a multiset, not by position: discoverAFP (and
// the other schemes) fan results in from concurrent per-interface/zone goroutines, so
// the same server set can land in a different order across two scans of an unchanged
// network. A positional compare would call that "changed" and fire a spurious SSE
// networks event + sidebar re-render every scanLoop tick even when nothing moved.
func volumeListsEqual(a, b []VolumeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v.ID]++
	}
	for _, v := range b {
		counts[v.ID]--
	}
	for _, n := range counts {
		if n != 0 {
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
