package router

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/pgodw/omnitalk/appletalk"
)

func UCase(input []byte) []byte {
	return appletalk.MacRomanToUpper(input)
}

type ZoneInformationTable struct {
	networkMinToMax         map[uint16]uint16
	networkMinToZones       map[uint16]map[string][]byte
	networkMinToDefaultZone map[uint16][]byte
	zoneToNetworkMins       map[string]map[uint16]struct{}
	ucaseToZone             map[string][]byte
	mu                      sync.RWMutex
}

func NewZoneInformationTable() *ZoneInformationTable {
	return &ZoneInformationTable{
		networkMinToMax:         map[uint16]uint16{},
		networkMinToZones:       map[uint16]map[string][]byte{},
		networkMinToDefaultZone: map[uint16][]byte{},
		zoneToNetworkMins:       map[string]map[uint16]struct{}{},
		ucaseToZone:             map[string][]byte{},
	}
}

func (z *ZoneInformationTable) checkRange(networkMin uint16, networkMax *uint16) (uint16, bool, error) {
	lookedUp, exists := z.networkMinToMax[networkMin]
	if networkMax == nil {
		if !exists {
			return 0, false, fmt.Errorf("network range %d-? does not exist", networkMin)
		}
		return lookedUp, true, nil
	}
	if exists && lookedUp == *networkMax {
		return *networkMax, true, nil
	}
	if exists {
		return 0, false, fmt.Errorf("network range overlaps existing")
	}
	for emn, emx := range z.networkMinToMax {
		if emn <= *networkMax && emx >= networkMin {
			return 0, false, fmt.Errorf("network range overlaps existing")
		}
	}
	return *networkMax, false, nil
}

func (z *ZoneInformationTable) AddNetworksToZone(zoneName []byte, networkMin uint16, networkMax *uint16) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if networkMax != nil && *networkMax < networkMin {
		return fmt.Errorf("range is backwards")
	}
	uc := string(UCase(zoneName))
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
			delete(z.ucaseToZone, string(UCase([]byte(key))))
		}
	}
	delete(z.networkMinToDefaultZone, networkMin)
	delete(z.networkMinToZones, networkMin)
	delete(z.networkMinToMax, networkMin)
	return nil
}

func (z *ZoneInformationTable) Zones() [][]byte {
	z.mu.RLock()
	defer z.mu.RUnlock()
	out := make([][]byte, 0, len(z.zoneToNetworkMins))
	for s := range z.zoneToNetworkMins {
		out = append(out, []byte(s))
	}
	return out
}

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

func (z *ZoneInformationTable) NetworksInZone(zoneName []byte) []uint16 {
	z.mu.RLock()
	defer z.mu.RUnlock()
	canonical := z.ucaseToZone[string(UCase(zoneName))]
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
