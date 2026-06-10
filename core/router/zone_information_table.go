// Zone Information Table: the network-range ⇄ zone-name associations ZIP builds
// and the router/ZIP service query. Re-expressed for the core ring — it uses
// core/encoding for MacRoman case-folding and carries no logging or port deps.
// (Behaviour mirrors the legacy router.ZoneInformationTable.)
package router

import (
	"bytes"
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
)

// ucase folds a zone name to upper case for case-insensitive comparison, using
// the AppleTalk MacRoman case table.
func ucase(input []byte) []byte {
	return encoding.MacRomanToUpper(input)
}

// Zone-table errors, returned by the range-checking helpers.
var (
	// ErrZoneRangeMissing reports a lookup of a network range that does not exist.
	ErrZoneRangeMissing = errors.New("router: network range does not exist")
	// ErrZoneRangeOverlap reports an add whose range overlaps an existing one.
	ErrZoneRangeOverlap = errors.New("router: network range overlaps existing")
	// ErrZoneRangeBackwards reports an add whose max precedes its min.
	ErrZoneRangeBackwards = errors.New("router: network range is backwards")
)

// ZoneInformationTable maps network ranges to the zones they belong to and back.
// Zone names are stored case-preserved but matched case-insensitively (MacRoman).
type ZoneInformationTable struct {
	mu                      sync.RWMutex
	networkMinToMax         map[uint16]uint16
	networkMinToZones       map[uint16]map[string][]byte
	networkMinToDefaultZone map[uint16][]byte
	zoneToNetworkMins       map[string]map[uint16]struct{}
	ucaseToZone             map[string][]byte
}

// NewZoneInformationTable builds an empty zone information table.
func NewZoneInformationTable() *ZoneInformationTable {
	return &ZoneInformationTable{
		networkMinToMax:         map[uint16]uint16{},
		networkMinToZones:       map[uint16]map[string][]byte{},
		networkMinToDefaultZone: map[uint16][]byte{},
		zoneToNetworkMins:       map[string]map[uint16]struct{}{},
		ucaseToZone:             map[string][]byte{},
	}
}

// checkRange validates a (min, max?) range against the table. When networkMax is
// nil it is a lookup (the range must exist). Otherwise it is an add: an exact
// existing match returns exists=true; any partial overlap is an error. Caller
// holds z.mu.
func (z *ZoneInformationTable) checkRange(networkMin uint16, networkMax *uint16) (uint16, bool, error) {
	lookedUp, exists := z.networkMinToMax[networkMin]
	if networkMax == nil {
		if !exists {
			return 0, false, ErrZoneRangeMissing
		}
		return lookedUp, true, nil
	}
	if exists && lookedUp == *networkMax {
		return *networkMax, true, nil
	}
	if exists {
		return 0, false, ErrZoneRangeOverlap
	}
	for emn, emx := range z.networkMinToMax {
		if emn <= *networkMax && emx >= networkMin {
			return 0, false, ErrZoneRangeOverlap
		}
	}
	return *networkMax, false, nil
}

// AddNetworksToZone associates a network range with a zone, creating the zone if
// new. The first zone added for a range becomes its default zone.
func (z *ZoneInformationTable) AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if networkMax != nil && *networkMax < networkMin {
		return ErrZoneRangeBackwards
	}
	uc := string(ucase(zoneName))
	if existing, ok := z.ucaseToZone[uc]; ok {
		zoneName = existing
	} else {
		z.ucaseToZone[uc] = append([]byte(nil), zoneName...)
		z.zoneToNetworkMins[string(zoneName)] = map[uint16]struct{}{}
	}
	rmax, exists, err := z.checkRange(networkMin, networkMax)
	if err != nil {
		return err
	}
	if !exists {
		z.networkMinToMax[networkMin] = rmax
		z.networkMinToZones[networkMin] = map[string][]byte{string(zoneName): append([]byte(nil), zoneName...)}
		z.networkMinToDefaultZone[networkMin] = append([]byte(nil), zoneName...)
	} else {
		z.networkMinToZones[networkMin][string(zoneName)] = append([]byte(nil), zoneName...)
	}
	z.zoneToNetworkMins[string(zoneName)][networkMin] = struct{}{}
	return nil
}

// RemoveNetworks drops a network range and its zone associations; a zone left
// with no networks is forgotten. A missing or zero range is a no-op.
func (z *ZoneInformationTable) RemoveNetworks(networkMin uint16, networkMax *uint16) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	rmax, exists, err := z.checkRange(networkMin, networkMax)
	if err != nil {
		return err
	}
	if !exists || rmax == 0 {
		return nil
	}
	for key := range z.networkMinToZones[networkMin] {
		m := z.zoneToNetworkMins[key]
		delete(m, networkMin)
		if len(m) == 0 {
			delete(z.zoneToNetworkMins, key)
			delete(z.ucaseToZone, string(ucase([]byte(key))))
		}
	}
	delete(z.networkMinToDefaultZone, networkMin)
	delete(z.networkMinToZones, networkMin)
	delete(z.networkMinToMax, networkMin)
	return nil
}

// Zones returns every known zone name.
func (z *ZoneInformationTable) Zones() [][]byte {
	z.mu.RLock()
	defer z.mu.RUnlock()
	out := make([][]byte, 0, len(z.zoneToNetworkMins))
	for s := range z.zoneToNetworkMins {
		out = append(out, []byte(s))
	}
	return out
}

// ZonesInNetworkRange returns the zones for a range, default zone first. A
// nonexistent range yields nil with no error.
func (z *ZoneInformationTable) ZonesInNetworkRange(networkMin uint16, networkMax *uint16) ([][]byte, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	_, exists, err := z.checkRange(networkMin, networkMax)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	def := z.networkMinToDefaultZone[networkMin]
	out := make([][]byte, 0, len(z.networkMinToZones[networkMin]))
	out = append(out, append([]byte(nil), def...))
	for _, v := range z.networkMinToZones[networkMin] {
		if bytes.Equal(v, def) {
			continue
		}
		out = append(out, append([]byte(nil), v...))
	}
	return out, nil
}

// NetworksInZone returns every network number in the zone (case-insensitive).
func (z *ZoneInformationTable) NetworksInZone(zoneName []byte) []uint16 {
	z.mu.RLock()
	defer z.mu.RUnlock()
	canonical := z.ucaseToZone[string(ucase(zoneName))]
	if canonical == nil {
		return nil
	}
	m := z.zoneToNetworkMins[string(canonical)]
	var out []uint16
	for nmin := range m {
		nmax := z.networkMinToMax[nmin]
		for n := nmin; n <= nmax; n++ {
			out = append(out, n)
		}
	}
	return out
}
