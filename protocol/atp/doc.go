// Package atp defines the AppleTalk Transaction Protocol wire format:
// header layout, control-bit constants, function codes, the TRel timeout
// indicator, and Marshal/Unmarshal helpers via pkg/binutil.
//
// This package is wire-format only — no I/O, no goroutines, no state.
// The transaction state machine (Endpoint, TCB/RspCB, retry/release
// timers) lives in service/atp.
//
// References:
//   - Inside Macintosh: Networking, Chapter 6
//     https://dev.os9.ca/techpubs/mac/Networking/Networking-143.html
//   - ATP packet format
//     https://dev.os9.ca/techpubs/mac/Networking/Networking-145.html
package atp
