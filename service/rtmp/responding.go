package rtmp

import (
	"context"
	"encoding/binary"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

type RespondingService struct {
	ch chan struct {
		d ddp.Datagram
		p port.Port
	}
	stop chan struct{}
	wg   sync.WaitGroup
}

func NewRespondingService() *RespondingService {
	return &RespondingService{
		ch: make(chan struct {
			d ddp.Datagram
			p port.Port
		}, 256),
		stop: make(chan struct{}),
	}
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
				d, rx := item.d, item.p
				if d.DDPType == DDPTypeData {
					if len(d.Data) < 4 {
						continue
					}
					senderNetwork := binary.BigEndian.Uint16(d.Data[0:2])
					if d.Data[2] != 8 {
						continue
					}
					senderNode := d.Data[3]
					data := d.Data[4:]
					var senderNetworkMin, senderNetworkMax uint16
					var rtmpVersion byte
					if rx.ExtendedNetwork() {
						if len(data) < 6 {
							continue
						}
						senderNetworkMin = binary.BigEndian.Uint16(data[0:2])
						if data[2] != 0x80 {
							continue
						}
						senderNetworkMax = binary.BigEndian.Uint16(data[3:5])
						rtmpVersion = data[5]
						data = data[6:] // skip sender's own extended tuple before neighbor tuples
					} else {
						if len(data) < 3 {
							continue
						}
						senderNetworkMin = senderNetwork
						senderNetworkMax = senderNetwork
						if binary.BigEndian.Uint16(data[0:2]) != 0 {
							continue
						}
						rtmpVersion = data[2]
						data = data[3:]
					}
					if rtmpVersion != Version {
						continue
					}
					if rx.NetworkMin() == 0 && rx.NetworkMax() == 0 {
						_ = rx.SetNetworkRange(senderNetworkMin, senderNetworkMax)
					}
					i := 0
					for i+3 <= len(data) {
						nmin := binary.BigEndian.Uint16(data[i : i+2])
						rd := data[i+2]
						i += 3
						extended := rd&0x80 != 0
						nmax := nmin
						dist := rd & 0x1F
						if extended {
							if i+3 > len(data) {
								break
							}
							nmax = binary.BigEndian.Uint16(data[i : i+2])
							i += 3
						}
						if dist >= 15 {
							r.RoutingMarkBad(nmin, nmax)
						} else {
							r.RoutingConsider(&service.RouteEntry{
								ExtendedNetwork: extended,
								NetworkMin:      nmin,
								NetworkMax:      nmax,
								Distance:        dist + 1,
								Port:            rx,
								NextNetwork:     senderNetwork,
								NextNode:        senderNode,
							})
						}
					}
				} else if d.DDPType == DDPTypeRequest && len(d.Data) > 0 {
					switch d.Data[0] {
					case FuncRequest:
						if rx.NetworkMin() == 0 || rx.NetworkMax() == 0 || d.HopCount != 0 {
							continue
						}
						resp := []byte{byte(rx.Network() >> 8), byte(rx.Network()), 8, rx.Node()}
						if rx.ExtendedNetwork() {
							resp = append(resp, byte(rx.NetworkMin()>>8), byte(rx.NetworkMin()), 0x80, byte(rx.NetworkMax()>>8), byte(rx.NetworkMax()), Version)
						}
						r.Reply(d, rx, DDPTypeData, resp)
					case FuncRDRSplitHorizon, FuncRDRNoSplitHorizon:
						split := d.Data[0] == FuncRDRSplitHorizon
						for _, dd := range makeRoutingTableDatagramData(r, rx, split) {
							r.Reply(d, rx, DDPTypeData, dd)
						}
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
