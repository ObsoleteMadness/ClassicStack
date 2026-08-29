// Package ddp is the Datagram Delivery Protocol datagram type and codec
// (§2/§12). It is pure and reflection-free; the link and bus interfaces
// reference the Datagram value type.
//
// Ring: CORE (stdlib only). The real codec lands in step B7 — it is the one bit
// of real protocol logic allowed in Phase 1.
package ddp
