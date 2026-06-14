package main

import (
	"github.com/ObsoleteMadness/ClassicStack/core/buf"
	_ "github.com/ObsoleteMadness/ClassicStack/core/component"
	_ "github.com/ObsoleteMadness/ClassicStack/core/link" // M1: decorators must stay TinyGo-clean

	// M2: every protocol codec must stay TinyGo-clean (stdlib only, no reflect),
	// so the embedded target can encode/decode wire formats.
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/macipx"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/nbp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/pap"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"

	// M3: the real ports (read loop + framing demux + router delivery) must stay
	// TinyGo-clean so an embedded build can move frames.
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"

	// M4: the real router (routing/zone tables + membership), the IPX/NetBEUI mini-
	// routers, and the DDP services (RTMP/ZIP/AEP) must stay TinyGo-clean so an
	// embedded build can route and answer protocol requests.
	_ "github.com/ObsoleteMadness/ClassicStack/core/router"
	_ "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	_ "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/aep"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/rtmp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/zip"

	// M5: the DDP services (NBP name-information, MacIP gateway, IPX gateway)
	// must stay TinyGo-clean so an embedded build can answer name lookups and
	// gateway protocol requests.
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/ipxgw"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/nbp"

	// M7: the file services (AFP/SMB command engines over the §9 fs seam + the
	// NetBIOS NBF and NBIPX session engines that carry SMB over NetBEUI and IPX +
	// the datagram-layer browser service) must stay TinyGo-clean so an embedded
	// build can serve files and browse over the legacy protocols.
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	_ "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/browser"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/mailslot"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/messenger"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/smb"

	// M1: the pure-Go pcapfile capture writer is required to be TinyGo-safe (§6f)
	// so non-pcap/embedded links can still emit a Wireshark-openable file.
	_ "github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
)

// main references the TinyGo-safe core subset so the gate has real code to
// compile and link. Touch a value from core/buf so the import is not elided.
func main() {
	// Print is the only side effect; on TinyGo this exercises the runtime.
	println("cs-tinygo: core/buf.FrameMax =", buf.FrameMax)
}
