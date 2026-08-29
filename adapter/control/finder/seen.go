package finder

import (
	"sort"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// LastSeen is GET /finder/discover: the last successful scan per scheme. Empty
// scheme returns every remembered client. The list is process-global so a web
// client that reloads paints instantly while POST /finder/discover scans again.
func (s *Service) LastSeen(scheme string) []VolumeInfo {
	s.mu.Lock()
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "*" {
		scheme = ""
	}
	var out []VolumeInfo
	if scheme == "" {
		n := 0
		for _, vols := range s.seen {
			n += len(vols)
		}
		out = make([]VolumeInfo, 0, n)
		for _, vols := range s.seen {
			out = append(out, vols...)
		}
	} else {
		src := s.seen[scheme]
		out = make([]VolumeInfo, len(src))
		copy(out, src)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Title != out[j].Title {
			return out[i].Title < out[j].Title
		}
		return out[i].ID < out[j].ID
	})
	s.log.Log2(log.Debug, "finder last-seen", log.Str("scheme", scheme), log.Int("count", int64(len(out))))
	return out
}

// remember replaces the last-seen list for one scheme after a successful scan.
func (s *Service) remember(scheme string, vols []VolumeInfo) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		return
	}
	vols = s.markOwnServers(scheme, vols)
	copied := make([]VolumeInfo, len(vols))
	copy(copied, vols)
	s.mu.Lock()
	prev := s.seen[scheme]
	s.seen[scheme] = copied
	total := 0
	for _, v := range s.seen {
		total += len(v)
	}
	s.mu.Unlock()
	s.log.Log(log.Debug, "finder remembered clients",
		log.Str("scheme", scheme), log.Int("count", int64(len(copied))), log.Int("total", int64(total)))
	if volumeListsEqual(prev, copied) {
		return
	}
	s.publishNetworks(scheme, copied)
}
