// Package protocol is the parent of the pure, reflection-free protocol codec
// packages (ddp and siblings: atp, asp, pap, nbp, ipx, netbeui, smb, netbios).
// A codec is not a service: it only encodes/decodes wire forms (§2/§12).
//
// Ring: CORE (stdlib only). The DDP codec (ddp) is the one piece of real logic
// allowed in Phase 1 (step B7); the siblings are stubs until Phase 2 (M2).
package protocol
