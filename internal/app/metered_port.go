package app

import (
	"sync/atomic"

	"github.com/ObsoleteMadness/ClassicStack/pkg/metrics"
	"github.com/ObsoleteMadness/ClassicStack/port"
)

// portMeter accumulates per-port rx/tx packet and byte counts from a port's
// TrafficObserver and publishes them to the metrics hub. One meter is created
// per metered port; the supervisor's refresh ticker calls publish() each
// second so the SSE broadcaster can derive per-second rates.
//
// Ports report traffic through the optional port.TrafficMetered interface, so
// no port implementation depends on pkg/metrics — the data path only calls a
// plain observer func, and the metrics wiring lives here in internal/app.
type portMeter struct {
	unit string

	rxPackets atomic.Int64
	rxBytes   atomic.Int64
	txPackets atomic.Int64
	txBytes   atomic.Int64
}

// newPortMeter returns a meter publishing under the given status-unit name
// (e.g. "EtherTalk", "LToUDP", "TashTalk").
func newPortMeter(unit string) *portMeter {
	return &portMeter{unit: unit}
}

// observe is the port.TrafficObserver installed on the port; it runs on the
// data path so it only does atomic adds.
func (m *portMeter) observe(dir port.Direction, bytes int) {
	switch dir {
	case port.Rx:
		m.rxPackets.Add(1)
		m.rxBytes.Add(int64(bytes))
	case port.Tx:
		m.txPackets.Add(1)
		m.txBytes.Add(int64(bytes))
	}
}

// publish pushes the current counter totals to the metrics hub under the
// "unit:<Name>:<metric>" namespace the dashboard reads.
func (m *portMeter) publish() {
	pushUnitCounter(m.unit, "rx.packets", m.rxPackets.Load())
	pushUnitCounter(m.unit, "rx.bytes", m.rxBytes.Load())
	pushUnitCounter(m.unit, "tx.packets", m.txPackets.Load())
	pushUnitCounter(m.unit, "tx.bytes", m.txBytes.Load())
}

// attachPortMeter installs a meter on p when it supports traffic metering,
// returning the meter so the supervisor can publish it each tick. Ports that
// do not implement port.TrafficMetered (e.g. test ports) yield a nil meter and
// simply report no throughput.
func attachPortMeter(unit string, p port.Port) *portMeter {
	tm, ok := p.(port.TrafficMetered)
	if !ok {
		return nil
	}
	m := newPortMeter(unit)
	tm.SetTrafficObserver(m.observe)
	return m
}

// pushUnitCounter publishes a counter sample under the "unit:<Name>:<metric>"
// namespace shared with the dashboard.
func pushUnitCounter(unit, metric string, value int64) {
	metrics.Push(metrics.Sample{
		Name:  unitMetricName(unit, metric),
		Value: value,
		Kind:  metrics.KindCounter,
	})
}

// unitMetricName builds the namespaced metric name the SPA matches per card.
func unitMetricName(unit, metric string) string {
	return "unit:" + unit + ":" + metric
}
