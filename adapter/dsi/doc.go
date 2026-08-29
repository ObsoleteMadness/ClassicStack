// Package dsi is the DSI-over-TCP session transport for AFP: it accepts TCP
// connections (conventionally :548), frames each as a stream of core/protocol/dsi
// headers + data, and drives the transport-agnostic afp.CommandHandler/CommandCircuit
// seam (core/service/afp/conn.go) — the AFP analogue of adapter/smbtcp driving
// smb.SessionConsumer. It is the "modern" AFP transport (TCP → DSI → AFP), the
// counterpart to the "classic" ASP-over-DDP transport that lives in core/service/afp
// itself.
//
// Ring: ADAPTER. It uses net (forbidden in core), so the listener lives here, not in
// core/service/afp — mirroring how pcap/serial device I/O and the SMB-TCP listener
// live in adapters. It reaches AFP only through the small CommandHandler/CommandCircuit
// interfaces, so it never imports the AFP command internals.
package dsi
