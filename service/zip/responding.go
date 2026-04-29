package zip

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"

	"github.com/pgodw/omnitalk/encoding"
	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/service"
)

type RespondingService struct {
	ch chan struct {
		d ddp.Datagram
		p port.Port
	}
	stop            chan struct{}
	pendingExtReply map[uint16]map[string]struct{} // network_min -> set of zone names
	wg              sync.WaitGroup
}

func NewRespondingService() *RespondingService {
	return &RespondingService{
		ch: make(chan struct {
			d ddp.Datagram
			p port.Port
		}, 256),
		stop:            make(chan struct{}),
		pendingExtReply: map[uint16]map[string]struct{}{},
	}
}

// multicastAddresser is a port that can compute EtherTalk multicast addresses.
type multicastAddresser interface {
	MulticastAddress(zoneName []byte) []byte
}

func (s *RespondingService) Start(ctx context.Context, r service.Router) error {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case item := <-s.ch:
				d := item.d
				rx := item.p
				switch d.DDPType {
				case DDPType:
					if len(d.Data) < 2 {
						continue
					}
					switch d.Data[0] {
					case FuncReply:
						s.handleReply(r, d, false)
					case FuncExtReply:
						s.handleExtReply(r, d)
					case FuncQuery:
						handleQuery(r, d, rx)
					case FuncGetNetInfoReq:
						handleGetNetInfo(r, d, rx)
					}
				case ATPDDPType:
					if len(d.Data) != 8 {
						continue
					}
					ctrl := d.Data[0]
					bitmap := d.Data[1]
					fn := d.Data[4]
					zero := d.Data[5]
					if ctrl != ATPFuncTReq || bitmap != 1 || zero != 0 {
						continue
					}
					switch fn {
					case ATPGetMyZone:
						handleGetMyZone(r, d, rx)
					case ATPGetZoneList:
						handleGetZoneList(r, d, rx, false)
					case ATPGetLocalZoneList:
						handleGetZoneList(r, d, rx, true)
					}
				}
			}
		}
	}()
	return nil
}

func (s *RespondingService) Stop() error {
	close(s.stop)
	s.wg.Wait()
	return nil
}
func (s *RespondingService) Inbound(d ddp.Datagram, p port.Port) {
	select {
	case s.ch <- struct {
		d ddp.Datagram
		p port.Port
	}{d: d, p: p}:
	default:
	}
}

// handleReply processes ZIP_FUNC_REPLY: immediately commit each (network, zone) tuple.
func (s *RespondingService) handleReply(r service.Router, d ddp.Datagram, _ bool) {
	data := d.Data[2:]
	for len(data) >= 3 {
		nmin := binary.BigEndian.Uint16(data[0:2])
		l := int(data[2])
		if len(data) < 3+l {
			break
		}
		zone := data[3 : 3+l]
		data = data[3+l:]
		if l == 0 {
			continue
		}
		entry, _ := r.RoutingGetByNetwork(nmin)
		if entry == nil {
			netlog.Warn("ZIP reply refers to a network range (starting with %d) with which we are not familiar", nmin)
			continue
		}
		nmax := entry.NetworkMax
		if err := r.AddNetworksToZone(append([]byte(nil), zone...), nmin, &nmax); err != nil {
			netlog.Warn("ZIP reply couldn't be added to zone information table: %v", err)
		}
	}
}

// handleExtReply processes ZIP_FUNC_EXT_REPLY: accumulate tuples until we have the
// expected count before committing.
func (s *RespondingService) handleExtReply(r service.Router, d ddp.Datagram) {
	if len(d.Data) < 2 {
		return
	}
	count := int(d.Data[1])
	data := d.Data[2:]

	var lastNmin uint16
	for len(data) >= 3 {
		nmin := binary.BigEndian.Uint16(data[0:2])
		l := int(data[2])
		if len(data) < 3+l {
			break
		}
		zone := data[3 : 3+l]
		data = data[3+l:]
		if l == 0 {
			continue
		}
		lastNmin = nmin
		if s.pendingExtReply[nmin] == nil {
			s.pendingExtReply[nmin] = map[string]struct{}{}
		}
		s.pendingExtReply[nmin][string(zone)] = struct{}{}
	}

	// When we've accumulated at least count zones for the last network seen, commit.
	if count >= 1 && len(s.pendingExtReply[lastNmin]) >= count {
		entry, _ := r.RoutingGetByNetwork(lastNmin)
		if entry != nil {
			nmax := entry.NetworkMax
			for zoneStr := range s.pendingExtReply[lastNmin] {
				z := []byte(zoneStr)
				if err := r.AddNetworksToZone(z, lastNmin, &nmax); err != nil {
					netlog.Warn("ZIP ext reply couldn't be added to zone information table: %v", err)
				}
			}
		}
		delete(s.pendingExtReply, lastNmin)
	}
}

// handleQuery responds to ZIP_FUNC_QUERY.
func handleQuery(r service.Router, d ddp.Datagram, rx port.Port) {
	if len(d.Data) < 2 {
		return
	}
	nc := int(d.Data[1])
	if len(d.Data) != 2+nc*2 {
		return
	}
	for i := 0; i < nc; i++ {
		req := binary.BigEndian.Uint16(d.Data[2+i*2 : 4+i*2])
		entry, _ := r.RoutingGetByNetwork(req)
		if entry == nil {
			continue
		}
		zones, err := r.ZonesInNetworkRange(entry.NetworkMin, nil)
		if err != nil || len(zones) == 0 {
			continue
		}
		// Send one or more EXT_REPLY datagrams.
		buf := []byte{FuncExtReply, byte(len(zones))}
		for _, z := range zones {
			item := make([]byte, 3+len(z))
			binary.BigEndian.PutUint16(item[0:2], entry.NetworkMin)
			item[2] = byte(len(z))
			copy(item[3:], z)
			if len(buf)+len(item) > ddp.MaxDataLength {
				r.Reply(d, rx, DDPType, buf)
				buf = []byte{FuncExtReply, byte(len(zones))}
			}
			buf = append(buf, item...)
		}
		if len(buf) > 2 {
			r.Reply(d, rx, DDPType, buf)
		}
	}
}

// handleGetNetInfo responds to ZIP_FUNC_GETNETINFO_REQUEST.
func handleGetNetInfo(r service.Router, d ddp.Datagram, rx port.Port) {
	if rx.Network() == 0 || rx.NetworkMin() == 0 || rx.NetworkMax() == 0 {
		return
	}
	if len(d.Data) < 7 {
		return
	}
	// Bytes 1-5 must be zero.
	if !bytes.Equal(d.Data[1:6], []byte{0, 0, 0, 0, 0}) {
		return
	}
	zoneLen := int(d.Data[6])
	if len(d.Data) < 7+zoneLen {
		return
	}
	givenZone := d.Data[7 : 7+zoneLen]

	nmax := rx.NetworkMax()
	zones, err := r.ZonesInNetworkRange(rx.NetworkMin(), &nmax)
	if err != nil {
		netlog.Warn("couldn't get zone names in port network range for GetNetInfo: %v", err)
		return
	}
	if len(zones) == 0 {
		return
	}

	flags := byte(GetNetInfoZoneInvalid | GetNetInfoOnlyOneZone)
	defaultZone := zones[0]
	var mcastAddr []byte
	if ma, ok := rx.(multicastAddresser); ok {
		mcastAddr = ma.MulticastAddress(defaultZone)
	}

	givenUC := string(toUCase(givenZone))
	for i, zone := range zones {
		if i == 1 {
			flags &^= GetNetInfoOnlyOneZone
		}
		if string(toUCase(zone)) == givenUC {
			flags &^= GetNetInfoZoneInvalid
			if ma, ok := rx.(multicastAddresser); ok {
				mcastAddr = ma.MulticastAddress(zone)
			}
		}
		if i > 0 && flags&GetNetInfoZoneInvalid == 0 {
			break // have cleared both flags we care about
		}
	}

	if len(mcastAddr) == 0 {
		flags |= GetNetInfoUseBroadcast
	}

	reply := []byte{FuncGetNetInfoRep, flags,
		byte(rx.NetworkMin() >> 8), byte(rx.NetworkMin()),
		byte(rx.NetworkMax() >> 8), byte(rx.NetworkMax()),
		byte(len(givenZone))}
	reply = append(reply, givenZone...)
	reply = append(reply, byte(len(mcastAddr)))
	reply = append(reply, mcastAddr...)
	if flags&GetNetInfoZoneInvalid != 0 {
		reply = append(reply, byte(len(defaultZone)))
		reply = append(reply, defaultZone...)
	}
	r.Reply(d, rx, DDPType, reply)
}

// handleGetMyZone responds to ATP GetMyZone.
func handleGetMyZone(r service.Router, d ddp.Datagram, rx port.Port) {
	tid := binary.BigEndian.Uint16(d.Data[2:4])
	entry, _ := r.RoutingGetByNetwork(d.SourceNetwork)
	if entry == nil {
		return
	}
	zones, err := r.ZonesInNetworkRange(entry.NetworkMin, nil)
	if err != nil || len(zones) == 0 {
		return
	}
	zone := zones[0]
	resp := []byte{ATPFuncTResp | ATPEOM, 0,
		byte(tid >> 8), byte(tid),
		0, 0,
		0, 1,
		byte(len(zone))}
	resp = append(resp, zone...)
	r.Reply(d, rx, ATPDDPType, resp)
}

// handleGetZoneList responds to ATP GetZoneList / GetLocalZones.
func handleGetZoneList(r service.Router, d ddp.Datagram, rx port.Port, local bool) {
	tid := binary.BigEndian.Uint16(d.Data[2:4])
	startIndex := int(binary.BigEndian.Uint16(d.Data[6:8])) // 1-relative

	var zones [][]byte
	if local {
		nmax := rx.NetworkMax()
		var err error
		zones, err = r.ZonesInNetworkRange(rx.NetworkMin(), &nmax)
		if err != nil {
			netlog.Warn("couldn't get zone names in port network range for GetLocalZones: %v", err)
			return
		}
	} else {
		zones = r.Zones()
	}

	// Skip startIndex-1 entries.
	if startIndex > 1 {
		skip := startIndex - 1
		if skip >= len(zones) {
			zones = nil
		} else {
			zones = zones[skip:]
		}
	}

	lastFlag := byte(0)
	var zoneList []byte
	numZones := 0
	const atpHdrLen = 8
	for i, zone := range zones {
		if atpHdrLen+len(zoneList)+1+len(zone) > ddp.MaxDataLength {
			break
		}
		zoneList = append(zoneList, byte(len(zone)))
		zoneList = append(zoneList, zone...)
		numZones++
		if i == len(zones)-1 {
			lastFlag = 1 // exhausted the list
		}
	}

	resp := []byte{ATPFuncTResp | ATPEOM, 0,
		byte(tid >> 8), byte(tid),
		lastFlag, 0,
		byte(numZones >> 8), byte(numZones)}
	resp = append(resp, zoneList...)
	r.Reply(d, rx, ATPDDPType, resp)
}

// toUCase uses the centralized MacRoman case-fold from the appletalk package.
func toUCase(input []byte) []byte {
	return encoding.MacRomanToUpper(input)
}
