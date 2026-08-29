// Package serial is the shared UART/serial-device opener: it turns a named serial
// interface (device path + baud) into an io.ReadWriteCloser byte stream, and nothing
// more. Ring: ADAPTER (platform concern — it links the host serial library).
//
// It is the §3b "shared serial opener" of the named-ports design (M11.c/D7): a
// `kind = "serial"` interface owns the device parameters, and a SINGLE opener
// returns the raw byte stream. The transport adapters that ride a serial line —
// tashtalk today; ppp/slip later — are then FRAMERS over that stream (each supplying
// its own escape/FCS rules) rather than each owning its own serial.Open. That keeps
// the device-open in one place and lets the compose layer dispatch on interface kind
// (nic → pcap, serial → this) instead of hard-wiring a medium per transport.
//
// This package does NOT present core/link.FrameLink — it is one layer below that. A
// caller wraps the returned stream in the matching framer (e.g. tashtalk.NewStream).
package serial
