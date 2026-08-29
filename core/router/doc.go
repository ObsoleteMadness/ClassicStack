// Package router defines the AppleTalk router membership API and the DDP data
// interface (RoutedPort) a routed port exposes to the router (§3). The router
// never knows whether a port's datagrams came from a kernel socket or from
// Framing(FrameLink).
//
// Ring: CORE (stdlib + core/component + core/protocol/ddp). The Phase 1
// placeholder Router lands in step D2; real RTMP/ZIP routing is Phase 2.
package router
