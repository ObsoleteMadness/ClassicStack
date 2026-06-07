/*
Command classicstack is the AppleTalk Phase 2 router and AFP file server.

It wires ports (EtherTalk, LToUDP, TashTalk, virtual LocalTalk) to a
router, registers the requested services (RTMP, ZIP, NBP, AEP, AFP over
ASP/DSI, MacIP), and runs until interrupted. Configuration comes from
flags and an optional TOML file; build tags (afp, macgarden, macip,
sqlite_cnid) gate the optional subsystems so a router-only binary
shrinks accordingly.

This package is a thin entry point: it holds the link-time build vars and
hands off to internal/app, which owns the run-core (flag/TOML parsing, the
Supervisor, and all service wiring) shared with the service/daemon wrappers
(cmd/classicstack-svc, cmd/classicstackd). Protocol logic lives under
protocol/, link-layer transports under port/, and stateful services under
service/.
*/
package main
