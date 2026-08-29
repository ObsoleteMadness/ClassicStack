package ncp

import (
	"strconv"
	"sync/atomic"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

// stats.go exposes the NCP service to the management plane (§5). The supervisor
// type-asserts the optional core/component capabilities and the SPA renders
// whatever it finds, so implementing them here is all it takes for the NCP card to
// appear on the dashboard with live metrics — no SPA changes. NCP implements:
//
//   - Describable (Kind + Props): identity/binding detail rows.
//   - Statful (Stats): monotonic counters + point-in-time gauges; the SPA derives
//     packets/sec & throughput from counter deltas between SSE samples.
//   - StatsEmitter (SetStatsSink): a push on connect/login so the
//     connected-machines / logged-in-users gauges update with low latency.
//   - Metered (SetTrafficObserver): the over-IPX transport reports per-datagram
//     byte counts so the dashboard shows packets/sec & throughput.

// counters holds the service's monotonic protocol counters. Updated via atomics so
// the hot path (datagram dispatch) does not contend on the service lock.
type counters struct {
	requestsRX    atomic.Uint64
	repliesTX     atomic.Uint64
	bytesRX       atomic.Uint64
	bytesTX       atomic.Uint64
	loginsOK      atomic.Uint64
	loginsFailed  atomic.Uint64
	decodeErrors  atomic.Uint64
	unsupportedFn atomic.Uint64
	sapBroadcasts atomic.Uint64
}

// --- component.Describable ---

// Kind labels the NCP component for the dashboard.
func (s *Service) Kind() string { return "service" }

// Props surfaces dashboard detail: the advertised server name, transport binding,
// SAP state, and live volume count, so the operator sees NCP's identity and
// binding without opening config.
func (s *Service) Props() map[string]string {
	s.mu.Lock()
	nvols := len(s.vols)
	sap := s.closersHaveSAPLocked()
	s.mu.Unlock()
	props := map[string]string{
		"volumes":   strconv.Itoa(nvols),
		"transport": "ipx:0451",
		"server":    s.serverName(),
	}
	if sap {
		props["sap"] = "advertising"
	} else {
		props["sap"] = "off"
	}
	return props
}

// closersHaveSAPLocked reports whether a SAP advertiser is installed (the over-IPX
// transport owns it). Caller holds s.mu.
func (s *Service) closersHaveSAPLocked() bool {
	for _, c := range s.closers {
		if a, ok := c.(sapAdvertiserState); ok && a.advertising() {
			return true
		}
	}
	return false
}

// sapAdvertiserState is the optional surface a closer exposes to report whether it
// is advertising via SAP (the over-IPX transport implements it).
type sapAdvertiserState interface{ advertising() bool }

// --- component.Statful ---

// Stats returns a point-in-time snapshot: monotonic protocol counters and the
// connection-table gauges (the operator's "who's on" view).
func (s *Service) Stats() component.Stats {
	conns, loggedIn, openFiles := s.conns.Snapshot()
	s.mu.Lock()
	nvols := len(s.vols)
	s.mu.Unlock()
	return component.Stats{
		Counters: map[string]uint64{
			"requests_rx":    s.counters.requestsRX.Load(),
			"replies_tx":     s.counters.repliesTX.Load(),
			"bytes_rx":       s.counters.bytesRX.Load(),
			"bytes_tx":       s.counters.bytesTX.Load(),
			"logins_ok":      s.counters.loginsOK.Load(),
			"logins_failed":  s.counters.loginsFailed.Load(),
			"decode_errors":  s.counters.decodeErrors.Load(),
			"unsupported_fn": s.counters.unsupportedFn.Load(),
			"sap_broadcasts": s.counters.sapBroadcasts.Load(),
		},
		Gauges: map[string]float64{
			"connected_machines": float64(conns),
			"logged_in_users":    float64(loggedIn),
			"open_files":         float64(openFiles),
			"volumes":            float64(nvols),
		},
	}
}

// --- component.StatsEmitter ---

// SetStatsSink installs the push sink the supervisor supplies (§5). NCP pushes a
// fresh snapshot on connection create/destroy and login so the gauges update with
// low latency. A nil sink clears it. Idempotent; safe before Start.
func (s *Service) SetStatsSink(sink func(component.Stats)) {
	s.mu.Lock()
	s.statsSink = sink
	s.mu.Unlock()
}

// pushStats sends a fresh snapshot to the push sink if one is installed.
func (s *Service) pushStats() {
	s.mu.Lock()
	sink := s.statsSink
	s.mu.Unlock()
	if sink != nil {
		sink(s.Stats())
	}
}

// --- component.Metered ---

// SetTrafficObserver installs the rx/tx byte observer the dashboard turns into
// packets/sec & throughput (§5). The over-IPX transport calls observeRX/observeTX
// per datagram. A nil observer disables metering. Idempotent; safe before Start.
func (s *Service) SetTrafficObserver(obs func(rxBytes, txBytes int)) {
	s.mu.Lock()
	s.rxObs = obs
	s.mu.Unlock()
}

// observeRX records an inbound datagram's byte count (counter + traffic observer).
func (s *Service) observeRX(n int) {
	s.counters.requestsRX.Add(1)
	s.counters.bytesRX.Add(uint64(n))
	s.mu.Lock()
	obs := s.rxObs
	s.mu.Unlock()
	if obs != nil {
		obs(n, 0)
	}
}

// observeTX records an outbound datagram's byte count (counter + traffic observer).
func (s *Service) observeTX(n int) {
	s.counters.repliesTX.Add(1)
	s.counters.bytesTX.Add(uint64(n))
	s.mu.Lock()
	obs := s.rxObs
	s.mu.Unlock()
	if obs != nil {
		obs(0, n)
	}
}

// compile-time assertions: the service implements the optional dashboard
// capabilities the supervisor discovers.
var (
	_ component.Describable  = (*Service)(nil)
	_ component.Statful      = (*Service)(nil)
	_ component.StatsEmitter = (*Service)(nil)
	_ component.Metered      = (*Service)(nil)
)
