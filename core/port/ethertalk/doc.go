// Package ethertalk is the EtherTalk (AppleTalk-over-Ethernet) port. In Phase 1
// it is a placeholder Component (via core/port/internal/portbase): it satisfies
// the lifecycle + Bindable/Statful/Configurable capabilities and no-ops the data
// path. The real port over a pcap/raw FrameLink lands in Phase 2.
//
// Ring: CORE (stdlib + core interfaces only).
package ethertalk
