//go:build afp || all

package afp

import "sync"

// desktopState owns the per-volume Desktop database handles and the
// DTRefNum → volume mapping handed out by FPOpenDT. The desktop subsystem
// only ever needs these three fields, so they sit behind their own
// RWMutex to keep AFP's auth / fork / volume call paths off the same
// contention domain.
type desktopState struct {
	mu        sync.RWMutex
	dbs       map[uint16]DesktopDB // volID → DesktopDB
	refs      map[uint16]uint16    // DTRefNum → volID
	nextDTRef uint16
}

func newDesktopState() desktopState {
	return desktopState{
		dbs:       make(map[uint16]DesktopDB),
		refs:      make(map[uint16]uint16),
		nextDTRef: 1,
	}
}

// volumeOf returns the volume id associated with a DTRefNum. The second
// result is false when the reference number was never issued or has been
// closed.
func (d *desktopState) volumeOf(dtRefNum uint16) (uint16, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	volID, ok := d.refs[dtRefNum]
	return volID, ok
}

// lookup returns the DesktopDB for the given DTRefNum and the volume id it
// was opened against. The bool reports whether the DTRefNum is known; the
// returned DesktopDB may still be nil when the ref exists but the
// per-volume DB has not been opened (e.g. tests stub the ref directly).
// Callers that need both must use lookupDB.
func (d *desktopState) lookup(dtRefNum uint16) (DesktopDB, uint16, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	volID, ok := d.refs[dtRefNum]
	if !ok {
		return nil, 0, false
	}
	return d.dbs[volID], volID, true
}

// lookupDB is the strict variant of lookup: it returns ok=false unless both
// the DTRefNum is known and a DesktopDB has been opened for its volume.
func (d *desktopState) lookupDB(dtRefNum uint16) (DesktopDB, uint16, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	volID, ok := d.refs[dtRefNum]
	if !ok {
		return nil, 0, false
	}
	db, ok := d.dbs[volID]
	if !ok {
		return nil, volID, false
	}
	return db, volID, true
}

// openRef registers a new DTRefNum for volID and returns it.
// loader is invoked exactly once per volume the first time openRef is called
// for that volume. It must not call back into desktopState.
func (d *desktopState) openRef(volID uint16, loader func() DesktopDB) uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, loaded := d.dbs[volID]; !loaded {
		d.dbs[volID] = loader()
	}
	ref := d.nextDTRef
	d.nextDTRef++
	d.refs[ref] = volID
	return ref
}

// closeRef invalidates a DTRefNum. It returns false when the reference was
// already closed or never existed.
func (d *desktopState) closeRef(dtRefNum uint16) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.refs[dtRefNum]; !ok {
		return false
	}
	delete(d.refs, dtRefNum)
	return true
}

// dbForVolume returns (and lazily creates via loader) the DesktopDB for
// volID. loader is invoked under the write lock and must not call back into
// desktopState.
func (d *desktopState) dbForVolume(volID uint16, loader func() DesktopDB) DesktopDB {
	d.mu.Lock()
	defer d.mu.Unlock()
	if db, ok := d.dbs[volID]; ok {
		return db
	}
	db := loader()
	if db == nil {
		return nil
	}
	d.dbs[volID] = db
	return db
}

// putDBForTest installs a DesktopDB directly. Tests use this to seed state
// without going through FPOpenDT.
func (d *desktopState) putDBForTest(volID uint16, db DesktopDB) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dbs[volID] = db
}

// putRefForTest installs a DTRefNum → volID mapping directly. Tests use this
// to short-circuit FPOpenDT.
func (d *desktopState) putRefForTest(dtRefNum, volID uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refs[dtRefNum] = volID
}

// dbCount returns the number of opened DesktopDBs. Tests use this to assert
// no persistence side-effects.
func (d *desktopState) dbCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.dbs)
}
