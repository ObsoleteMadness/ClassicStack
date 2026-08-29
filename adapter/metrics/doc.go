// Package metrics is an optional, build-tag-gated telemetry SINK: it subscribes to the
// stats topic of the telemetry bus and republishes every component's counters and
// gauges as Go expvar variables, the standard scrape surface external collectors read
// (Prometheus, a PerfMon HTTP data collector, or `go tool`).
//
// It is the additive "extra sink" half of the §5 stats design: the bus already fans
// StatSample to any subscriber, so this neither changes the producers nor the existing
// HTTP/ubus front-ends — it is one more reader. It is gated behind the `perfcounters`
// build tag (see sink.go / sink_stub.go) so the default build carries no expvar export;
// a Windows/edge build that wants a counter-scrape surface opts in.
//
// Ring: ADAPTER. expvar is stdlib (reflection-free for our use); no cgo, no PDH.
package metrics
