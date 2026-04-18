package llap

import (
	"fmt"
	"math/bits"
	"math/rand"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/go/appletalk"
	"github.com/pgodw/omnitalk/go/netlog"
	"github.com/pgodw/omnitalk/go/port"
	"github.com/pgodw/omnitalk/go/port/localtalk"
	"github.com/pgodw/omnitalk/go/service"
)

const (
	defaultProbeInterval = 250 * time.Millisecond
	probeAttemptsToClaim = 8
	maxRetries           = 32
	approxIDG            = 4 * time.Millisecond
	approxIFG            = 2 * time.Millisecond
	approxSlotTime       = 1 * time.Millisecond
	localTalkBitRate     = 230400
)

type ddpInboundRouter interface {
	service.Router
	Inbound(datagram appletalk.Datagram, rxPort port.Port)
}

type Service struct {
	stop   chan struct{}
	router ddpInboundRouter

	mu    sync.Mutex
	ports map[*localtalk.Port]*portState
	rand  *rand.Rand
}

type portState struct {
	port *localtalk.Port

	mu               sync.Mutex
	started          bool
	claimed          bool
	probeAttempts    int
	backoff          int
	deferHistory     uint8
	collisionHistory uint8
	lastActivity     time.Time
	expectCTSFrom    uint8
	ctsCh            chan struct{}
	txMu             sync.Mutex
	stop             chan struct{}
}

func New() *Service {
	return &Service{
		stop:  make(chan struct{}),
		ports: make(map[*localtalk.Port]*portState),
		rand:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *Service) Start(router service.Router) error {
	r, ok := router.(ddpInboundRouter)
	if !ok {
		return fmt.Errorf("llap: router does not support inbound datagram delivery")
	}
	s.router = r
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.ports {
		s.startPortLocked(st)
	}
	return nil
}

func (s *Service) Stop() error {
	close(s.stop)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.ports {
		close(st.stop)
	}
	return nil
}

func (s *Service) Inbound(_ appletalk.Datagram, _ port.Port) {}

func (s *Service) RegisterPort(p *localtalk.Port) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ports[p]; ok {
		return
	}
	st := &portState{port: p, stop: make(chan struct{}), lastActivity: time.Now()}
	s.ports[p] = st
	if s.router != nil {
		s.startPortLocked(st)
	}
	netlog.Info("[LLAP] attached to %s", p.ShortString())
}

func (s *Service) InboundFrame(p *localtalk.Port, frame localtalk.LLAPFrame) {
	if err := frame.Validate(); err != nil {
		netlog.Debug("[LLAP] %s dropped malformed frame type=0x%02X: %v", p.ShortString(), frame.Type, err)
		return
	}
	st := s.stateFor(p)
	st.noteFrameActivity(frame)
	if frame.IsData() {
		if s.router == nil {
			netlog.Debug("[LLAP] %s dropping inbound data frame while service router is uninitialized", p.ShortString())
			return
		}
		d, err := p.ParseInboundDataFrame(frame)
		if err != nil {
			netlog.Debug("[LLAP] %s failed to decode inbound data frame type=0x%02X: %v", p.ShortString(), frame.Type, err)
			return
		}
		netlog.LogDatagramInbound(p.Network(), p.Node(), d, p)
		s.router.Inbound(d, p)
		return
	}

	switch frame.Type {
	case localtalk.LLAPTypeENQ:
		s.handleENQ(st, frame)
	case localtalk.LLAPTypeACK:
		s.handleACK(st, frame)
	case localtalk.LLAPTypeRTS:
		s.handleRTS(st, frame)
	case localtalk.LLAPTypeCTS:
		s.handleCTS(st, frame)
	default:
		netlog.Debug("[LLAP] %s dropped invalid control type 0x%02X from node %d", p.ShortString(), frame.Type, frame.SourceNode)
	}
}

func (s *Service) TransmitUnicast(p *localtalk.Port, network uint16, node uint8, d appletalk.Datagram) {
	if network != 0 && network != p.Network() {
		netlog.Debug("[LLAP] %s dropping unicast to network=%d local-network=%d", p.ShortString(), network, p.Network())
		return
	}
	st := s.stateFor(p)
	if !st.isClaimed() {
		netlog.Debug("[LLAP] %s dropping unicast while node is unclaimed", p.ShortString())
		return
	}
	st.txMu.Lock()
	defer st.txMu.Unlock()
	netlog.LogDatagramUnicast(network, node, d, p)
	frame, err := p.BuildDataFrame(node, d)
	if err != nil {
		netlog.Warn("[LLAP] %s failed to build unicast frame to node %d: %v", p.ShortString(), node, err)
		return
	}
	if !p.SupportsRTSCTS() {
		if err := s.runDatagramTransmit(st, frame); err != nil {
			netlog.Warn("[LLAP] %s unicast transmit failed to node %d: %v", p.ShortString(), node, err)
		}
		return
	}
	if err := s.runDirectedTransmit(st, frame); err != nil {
		netlog.Warn("[LLAP] %s unicast transmit failed to node %d: %v", p.ShortString(), node, err)
	}
}

func (s *Service) TransmitBroadcast(p *localtalk.Port, d appletalk.Datagram) {
	st := s.stateFor(p)
	if !st.isClaimed() {
		netlog.Debug("[LLAP] %s dropping broadcast while node is unclaimed", p.ShortString())
		return
	}
	st.txMu.Lock()
	defer st.txMu.Unlock()
	netlog.LogDatagramBroadcast(d, p)
	frame, err := p.BuildDataFrame(localtalk.LLAPBroadcastNode, d)
	if err != nil {
		netlog.Warn("[LLAP] %s failed to build broadcast frame: %v", p.ShortString(), err)
		return
	}
	if err := s.runBroadcastTransmit(st, frame); err != nil {
		netlog.Warn("[LLAP] %s broadcast transmit failed: %v", p.ShortString(), err)
	}
}

func (s *Service) startPortLocked(st *portState) {
	if st.started {
		return
	}
	st.started = true
	go s.acquireLoop(st)
}

func (s *Service) acquireLoop(st *portState) {
	ticker := time.NewTicker(defaultProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-st.stop:
			return
		case <-ticker.C:
			st.mu.Lock()
			if st.claimed {
				st.mu.Unlock()
				return
			}
			if st.probeAttempts >= probeAttemptsToClaim {
				desired := st.port.DesiredNode()
				st.claimed = true
				st.port.ClaimNode(desired)
				st.mu.Unlock()
				netlog.Info("[LLAP] %s claimed node %d after %d ENQ probes", st.port.ShortString(), desired, probeAttemptsToClaim)
				return
			}
			desired := st.port.DesiredNode()
			st.probeAttempts++
			attempt := st.probeAttempts
			st.mu.Unlock()
			frame := localtalk.LLAPFrame{DestinationNode: desired, SourceNode: desired, Type: localtalk.LLAPTypeENQ}
			if err := s.sendFrame(st, frame); err != nil {
				netlog.Warn("[LLAP] %s failed to send ENQ probe for node %d: %v", st.port.ShortString(), desired, err)
				continue
			}
			netlog.Debug("[LLAP] %s ENQ probe attempt=%d desired=%d", st.port.ShortString(), attempt, desired)
		}
	}
}

func (s *Service) handleENQ(st *portState, frame localtalk.LLAPFrame) {
	if st.port.RespondToENQ() && st.port.ClaimedNode() != 0 && frame.DestinationNode == st.port.ClaimedNode() {
		ack := localtalk.LLAPFrame{DestinationNode: st.port.ClaimedNode(), SourceNode: st.port.ClaimedNode(), Type: localtalk.LLAPTypeACK}
		if err := s.sendFrame(st, ack); err != nil {
			netlog.Warn("[LLAP] %s failed to send ACK for ENQ on node %d: %v", st.port.ShortString(), frame.DestinationNode, err)
			return
		}
		netlog.Debug("[LLAP] %s ACK sent for ENQ on claimed node %d", st.port.ShortString(), frame.DestinationNode)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.claimed || frame.DestinationNode != st.port.DesiredNode() {
		return
	}
	oldDesired := st.port.DesiredNode()
	newDesired := st.port.RerollDesiredNode()
	st.probeAttempts = 0
	netlog.Info("[LLAP] %s rerolled desired node after ENQ collision old=%d new=%d", st.port.ShortString(), oldDesired, newDesired)
}

func (s *Service) handleACK(st *portState, frame localtalk.LLAPFrame) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.claimed || frame.DestinationNode != st.port.DesiredNode() {
		return
	}
	oldDesired := st.port.DesiredNode()
	newDesired := st.port.RerollDesiredNode()
	st.probeAttempts = 0
	netlog.Info("[LLAP] %s rerolled desired node after ACK collision old=%d new=%d", st.port.ShortString(), oldDesired, newDesired)
}

func (s *Service) handleRTS(st *portState, frame localtalk.LLAPFrame) {
	if !st.isClaimed() || frame.DestinationNode != st.port.ClaimedNode() {
		return
	}
	cts := localtalk.LLAPFrame{DestinationNode: frame.SourceNode, SourceNode: st.port.ClaimedNode(), Type: localtalk.LLAPTypeCTS}
	if err := s.sendFrame(st, cts); err != nil {
		netlog.Warn("[LLAP] %s failed to send CTS to node %d: %v", st.port.ShortString(), frame.SourceNode, err)
		return
	}
	netlog.Debug("[LLAP] %s CTS sent to node %d", st.port.ShortString(), frame.SourceNode)
}

func (s *Service) handleCTS(st *portState, frame localtalk.LLAPFrame) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.expectCTSFrom == 0 || st.expectCTSFrom != frame.SourceNode || st.ctsCh == nil {
		return
	}
	select {
	case st.ctsCh <- struct{}{}:
	default:
	}
	netlog.Debug("[LLAP] %s CTS received from node %d", st.port.ShortString(), frame.SourceNode)
}

func (s *Service) runDirectedTransmit(st *portState, frame localtalk.LLAPFrame) error {
	localBackoff := s.beginTransmit(st)
	defer s.finishTransmit(st)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		deferred := s.waitForIdle(st, localBackoff)
		if deferred {
			st.mu.Lock()
			st.deferHistory |= 1
			deferHistory := st.deferHistory
			st.mu.Unlock()
			netlog.Debug("[LLAP] %s transmit defer attempt=%d local-backoff=%d defer-history=%08b", st.port.ShortString(), attempt, localBackoff, deferHistory)
		}
		rts := localtalk.LLAPFrame{DestinationNode: frame.DestinationNode, SourceNode: st.port.ClaimedNode(), Type: localtalk.LLAPTypeRTS}
		if err := s.sendFrame(st, rts); err != nil {
			return err
		}
		st.mu.Lock()
		st.expectCTSFrom = frame.DestinationNode
		st.ctsCh = make(chan struct{}, 1)
		ctsCh := st.ctsCh
		st.mu.Unlock()
		select {
		case <-ctsCh:
			if err := s.sendFrame(st, frame); err != nil {
				return err
			}
			netlog.Debug("[LLAP] %s transmit success dst=%d attempt=%d local-backoff=%d", st.port.ShortString(), frame.DestinationNode, attempt, localBackoff)
			return nil
		case <-time.After(approxIFG):
			st.mu.Lock()
			st.collisionHistory |= 1
			collisionHistory := st.collisionHistory
			st.expectCTSFrom = 0
			st.ctsCh = nil
			st.mu.Unlock()
			oldBackoff := localBackoff
			localBackoff = minInt(maxInt(localBackoff*2, 2), 16)
			netlog.Debug("[LLAP] %s CTS timeout retry=%d dst=%d local-backoff=%d->%d collision-history=%08b", st.port.ShortString(), attempt, frame.DestinationNode, oldBackoff, localBackoff, collisionHistory)
		}
	}
	netlog.Warn("[LLAP] %s transmit failed after %d retries dst=%d", st.port.ShortString(), maxRetries, frame.DestinationNode)
	return fmt.Errorf("llap: retry limit exceeded")
}

func (s *Service) runDatagramTransmit(st *portState, frame localtalk.LLAPFrame) error {
	localBackoff := s.beginTransmit(st)
	defer s.finishTransmit(st)
	deferred := s.waitForIdle(st, localBackoff)
	if deferred {
		st.mu.Lock()
		st.deferHistory |= 1
		deferHistory := st.deferHistory
		st.mu.Unlock()
		netlog.Debug("[LLAP] %s datagram defer local-backoff=%d defer-history=%08b", st.port.ShortString(), localBackoff, deferHistory)
	}
	if err := s.sendFrame(st, frame); err != nil {
		return err
	}
	netlog.Debug("[LLAP] %s datagram transmit success dst=%d local-backoff=%d", st.port.ShortString(), frame.DestinationNode, localBackoff)
	return nil
}

func (s *Service) runBroadcastTransmit(st *portState, frame localtalk.LLAPFrame) error {
	localBackoff := s.beginTransmit(st)
	defer s.finishTransmit(st)
	deferred := s.waitForIdle(st, localBackoff)
	if deferred {
		st.mu.Lock()
		st.deferHistory |= 1
		deferHistory := st.deferHistory
		st.mu.Unlock()
		netlog.Debug("[LLAP] %s broadcast defer local-backoff=%d defer-history=%08b", st.port.ShortString(), localBackoff, deferHistory)
	}
	rts := localtalk.LLAPFrame{DestinationNode: localtalk.LLAPBroadcastNode, SourceNode: st.port.ClaimedNode(), Type: localtalk.LLAPTypeRTS}
	if err := s.sendFrame(st, rts); err != nil {
		return err
	}
	time.Sleep(approxIFG)
	if err := s.sendFrame(st, frame); err != nil {
		return err
	}
	netlog.Debug("[LLAP] %s broadcast transmit success local-backoff=%d", st.port.ShortString(), localBackoff)
	return nil
}

func (s *Service) sendFrame(st *portState, frame localtalk.LLAPFrame) error {
	if err := st.port.SendRawLLAPFrame(frame); err != nil {
		return err
	}
	st.noteFrameActivity(frame)
	return nil
}

func (s *Service) beginTransmit(st *portState) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	oldBackoff := st.backoff
	if bits.OnesCount8(st.collisionHistory) > 2 {
		st.backoff = minInt(maxInt(st.backoff*2, 2), 16)
		st.collisionHistory = 0
	} else if bits.OnesCount8(st.deferHistory) < 2 {
		st.backoff = st.backoff / 2
		st.deferHistory = 0
	}
	st.deferHistory <<= 1
	st.collisionHistory <<= 1
	if oldBackoff != st.backoff {
		netlog.Debug("[LLAP] %s backoff adjusted global=%d->%d defer-history=%08b collision-history=%08b", st.port.ShortString(), oldBackoff, st.backoff, st.deferHistory, st.collisionHistory)
	}
	return st.backoff
}

func (s *Service) finishTransmit(st *portState) {
	st.mu.Lock()
	st.expectCTSFrom = 0
	st.ctsCh = nil
	st.mu.Unlock()
}

func (s *Service) waitForIdle(st *portState, localBackoff int) bool {
	deferred := false
	for {
		st.mu.Lock()
		idleFor := time.Since(st.lastActivity)
		st.mu.Unlock()
		if idleFor >= approxIDG {
			break
		}
		deferred = true
		time.Sleep(approxIDG - idleFor)
	}
	if localBackoff <= 0 {
		return deferred
	}
	s.mu.Lock()
	slots := s.rand.Intn(localBackoff)
	s.mu.Unlock()
	if slots > 0 {
		deferred = true
		time.Sleep(time.Duration(slots) * approxSlotTime)
	}
	return deferred
}

func (s *Service) stateFor(p *localtalk.Port) *portState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.ports[p]; ok {
		return st
	}
	st := &portState{port: p, stop: make(chan struct{}), lastActivity: time.Now()}
	s.ports[p] = st
	if s.router != nil {
		s.startPortLocked(st)
	}
	return st
}

func (st *portState) noteFrameActivity(frame localtalk.LLAPFrame) {
	st.mu.Lock()
	busyUntil := time.Now().Add(frameTransmitDuration(frame))
	if busyUntil.After(st.lastActivity) {
		st.lastActivity = busyUntil
	}
	st.mu.Unlock()
}

func (st *portState) isClaimed() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.claimed || st.port.ClaimedNode() != 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func frameTransmitDuration(frame localtalk.LLAPFrame) time.Duration {
	bytesOnWire := len(frame.Bytes())
	if bytesOnWire <= 0 {
		return 0
	}
	return time.Duration(bytesOnWire*8) * time.Second / localTalkBitRate
}
