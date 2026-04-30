//go:build afp || all

package afp

import "sync"

// backupDates holds FPSetVolParms-supplied backup dates per volume. AFP 2.x
// §5.1.32 lets clients write a 32-bit "backup date" against a volume; we
// remember it so subsequent FPGetVolParms returns the same value.
//
// This is the only volume-related field that mutates after Service.Start.
// The Volumes slice and the volumeFS / metas / cnidStores maps are
// populated once during installVolumes and read-only thereafter, so they
// need no synchronisation. backupDates carries its own mutex so the
// FPSetVolParms write path no longer contends with fork, desktop, or auth
// traffic.
type backupDates struct {
	mu sync.RWMutex
	m  map[uint16]uint32
}

func newBackupDates() backupDates {
	return backupDates{m: make(map[uint16]uint32)}
}

// get returns the recorded backup date for volID, or zero when none has
// been set.
func (b *backupDates) get(volID uint16) uint32 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.m[volID]
}

// set records when as the backup date for volID.
func (b *backupDates) set(volID uint16, when uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[volID] = when
}
