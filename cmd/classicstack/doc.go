/*
Command classicstack is the AppleTalk Phase 2 router and AFP/SMB file server.

It loads server.toml into the config model, builds and supervises the compose
runtime (ports → router → services, cross-wired through the transport seams),
optionally serves the web-admin control API, and runs until interrupted.
Configuration is the named-instance TOML model; build tags (afp, smb, netbios,
ipx, netbeui, macip, pcap, …) gate the optional subsystems so a router-only
binary shrinks accordingly.

This package is a thin entry point: it holds the link-time build vars and hands
off to cmd/internal/cli, the shared new-ring run-core (config load, compose
runtime build/supervise, control plane) that the service/daemon wrappers
(cmd/classicstack-svc, cmd/classicstackd) share. Protocol logic lives under
core/protocol/, link-layer transports under core/port/ + adapter/link/, and
stateful services under core/service/.
*/
package main
