/*
Command omnitalk is the AppleTalk Phase 2 router and AFP file server.

It wires ports (EtherTalk, LToUDP, TashTalk, virtual LocalTalk) to a
router, registers the requested services (RTMP, ZIP, NBP, AEP, AFP over
ASP/DSI, MacIP), and runs until interrupted. Configuration comes from
flags and an optional TOML file; build tags (afp, macgarden, macip,
sqlite_cnid) gate the optional subsystems so a router-only binary
shrinks accordingly.

This package is the wiring layer only — protocol logic lives under
protocol/, link-layer transports under port/, and stateful services
under service/.
*/
package main
