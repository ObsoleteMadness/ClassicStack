// pcapdiff compares two pcap captures of AppleTalk traffic and reports
// per-side counts plus a packet-by-packet timeline annotated with DDP
// type, ATP function, ATP transaction ID, and ASP/AFP-style command.
//
// Inputs may be DLT_LTALK (LLAP+DDP, what classicstack writes for
// LocalTalk) or DLT_EN10MB (EtherTalk SNAP frames). The two files do
// not need to share a clock; alignment is by sequence, not absolute
// time.
//
// This is intentionally pragmatic — full AFP-level decoding is left to
// Wireshark/tshark. The tool's job is to surface "what conversations
// happened" so a human (or Claude in a follow-up session) can spot
// behavioural divergence.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	format := flag.String("format", "text", "Output format: text or json")
	limit := flag.Int("limit", 0, "Limit timeline output to N events per side (0 = unlimited)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: pcapdiff [-format text|json] [-limit N] <left.pcap> <right.pcap>\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}

	left, err := decodePcap(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "left: %v\n", err)
		os.Exit(1)
	}
	right, err := decodePcap(flag.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "right: %v\n", err)
		os.Exit(1)
	}

	switch *format {
	case "text":
		renderText(os.Stdout, flag.Arg(0), flag.Arg(1), left, right, *limit)
	case "json":
		renderJSON(os.Stdout, flag.Arg(0), flag.Arg(1), left, right, *limit)
	default:
		fmt.Fprintf(os.Stderr, "unknown -format %q\n", *format)
		os.Exit(2)
	}
}
