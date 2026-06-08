// Package port is the parent of the per-transport port packages (ethertalk,
// localtalk, ipx, netbeui). Each subpackage holds a Component that takes a
// FrameLink/DatagramLink and plugs into the router.
//
// Ring: CORE (stdlib + core interfaces). Phase 1 placeholders land in step D1;
// real ports over real links are Phase 2.
package port
