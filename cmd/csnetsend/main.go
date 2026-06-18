// Command csnetsend is a standalone NetBIOS Messenger ("net send" / WinPopup)
// payload builder. It is a T1 "protocol-reuse proof": it drives the SAME core codecs
// the messenger service uses — core/protocol/messenger (the single-block message
// frame) wrapped in core/protocol/mailslot (the \MAILSLOT\MESSNGR SMB_COM_TRANSACTION
// envelope) — to assemble exactly the bytes a "net send" puts on the wire, then
// prints a hex dump and optionally writes them to a file.
//
// Scope (per the M7g/T1 decision "core send half only"): this builds the mailslot
// PAYLOAD. The outer NetBIOS DATAGRAM framing and the transport (NBT UDP/138,
// NetBEUI, or NB-IPX) are a NetBIOS-transport concern (M7b2) the messenger service
// reaches through netbios.SendDatagram; csnetsend stops at the payload the service
// would hand that seam, which is the genuinely protocol-layer, transport-free half.
package main

import (
	"flag"
	"fmt"
	"os"

	mailslot "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	messenger "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csnetsend:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		from = flag.String("from", "CLASSICSTACK", "sender name (the From field)")
		to   = flag.String("to", "", "recipient name (the To field; required)")
		text = flag.String("text", "", "message text (required)")
		out  = flag.String("o", "", "write the raw payload bytes to this file (optional)")
	)
	flag.Parse()

	if *to == "" || *text == "" {
		flag.Usage()
		return fmt.Errorf("both -to and -text are required")
	}

	// Build the inner single-block messenger frame (From/To/Text as NUL-terminated
	// OEM strings), then wrap it in the \MAILSLOT\MESSNGR transaction envelope — the
	// exact two-layer construction messenger.Service.SendMessage performs before
	// handing the bytes to the NetBIOS datagram seam.
	body := messenger.Message{From: *from, To: *to, Text: *text}.Marshal()
	payload := mailslot.Write{Name: mailslot.NameMessenger, Body: body}.Marshal()

	fmt.Printf("messenger frame: %d bytes (From=%q To=%q)\n", len(body), *from, *to)
	fmt.Printf("\\MAILSLOT\\MESSNGR transaction: %d bytes\n\n", len(payload))
	fmt.Print(hexDump(payload))

	if *out != "" {
		if err := os.WriteFile(*out, payload, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", *out, err)
		}
		fmt.Printf("\nwrote %d bytes to %s\n", len(payload), *out)
	}
	return nil
}

// hexDump renders b as a classic offset/hex/ASCII dump (16 bytes per row), so the
// assembled payload can be eyeballed or diffed against a capture.
func hexDump(b []byte) string {
	const perRow = 16
	var sb []byte
	for off := 0; off < len(b); off += perRow {
		end := min(off+perRow, len(b))
		row := b[off:end]
		sb = fmt.Appendf(sb, "%08x  ", off)
		for i := range perRow {
			if i < len(row) {
				sb = fmt.Appendf(sb, "%02x ", row[i])
			} else {
				sb = append(sb, ' ', ' ', ' ')
			}
		}
		sb = append(sb, ' ', '|')
		for _, c := range row {
			if c >= 0x20 && c < 0x7f {
				sb = append(sb, c)
			} else {
				sb = append(sb, '.')
			}
		}
		sb = append(sb, '|', '\n')
	}
	return string(sb)
}
