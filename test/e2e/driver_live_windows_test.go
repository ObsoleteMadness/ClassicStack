//go:build windows && driverint

package e2e

// driver_live_windows_test.go drives the REAL raw-Ethernet SMB transports (direct-IPX,
// NBIPX, NBF) client↔server over an actual Npcap-captured segment — the wire path the
// in-process harness cannot exercise (it uses an inmem pair; here real frames cross a real
// NDIS adapter through the Npcap driver). The server is an in-process classicstack SMB
// service on a genuine core port bound to the segment device; the client is the genuine
// clientsmb dialer bound to the SAME device. Both put frames on the wire via Npcap, so
// this proves the send path (which fails on loopback) end to end.
//
// AFP-over-EtherTalk is intentionally not covered here: its server side needs the full
// EtherTalk port + RTMP/ZIP/AARP node-claim + NBP resolution stack, far heavier than the
// SMB carriers, and AFP's DDP semantics are already covered in-process. The raw-Ethernet
// gap these tests close is specifically the Npcap send path shared by all raw carriers.

import (
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	corenb "github.com/ObsoleteMadness/ClassicStack/core/port"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	nbfport "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	netbeuirouter "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"
	netbiossvc "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	smbsvc "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// driverServerMAC / driverServerMACnbf are the server station addresses on the segment.
// On Ethernet the IPX node IS the MAC, so the router identity uses it and the client's
// directed frames pass the addressed-to-us filter.
var (
	driverServerMAC    = [6]byte{0x02, 0x00, 0x00, 0x00, 0xCD, 0x01} // IPX / NBIPX
	driverServerMACnbf = [6]byte{0x02, 0x00, 0x00, 0x00, 0xCD, 0x02} // NBF (LLC2)
)

// rawFrameLink opens an unfiltered raw pcap FrameLink on dev. The EtherTalk BPF preset
// would drop IPX/NBF, so raw carriers must open with no filter (see
// npcap-loopback-vs-virtual-nic memory / spec).
func rawFrameLink(dev string) (link.FrameLink, error) {
	return pcap.Open(pcap.Config{
		Interface:     dev,
		SnapLen:       65535,
		Promiscuous:   true,
		ImmediateMode: true,
		ReadTimeout:   200 * time.Millisecond,
	})
}

// TestDriverLiveSMBRawEthernet runs the file-operation battery over each raw-Ethernet SMB
// carrier, client and server both bound to the acquired segment via Npcap.
func TestDriverLiveSMBRawEthernet(t *testing.T) {
	requireDriverEnv(t)
	seg := acquireSegment(t)

	t.Run("smb/nbipx", func(t *testing.T) {
		remote := driverSMBNBIPX(t, seg.dev)
		exerciseFileOps(t, remote, longNames)
	})
	t.Run("smb/nbf", func(t *testing.T) {
		remote := driverSMBNBF(t, seg.dev)
		exerciseFileOps(t, remote, longNames)
	})
}

// driverSMBService builds and starts an in-process SMB service with one memfs share, plus
// the NetBIOS service that fronts it. Shared by the NBIPX and NBF live builders.
func driverSMBService(t *testing.T) (*smbsvc.Service, *netbiossvc.Service) {
	t.Helper()
	sm, err := smbsvc.NewWithShares(nil, smbsvc.ShareSpec{Name: "Share", Share: memShare("Share")})
	if err != nil {
		t.Fatalf("smb NewWithShares: %v", err)
	}
	if err := sm.Start(context.Background()); err != nil {
		t.Fatalf("smb Start: %v", err)
	}
	t.Cleanup(func() { _ = sm.Stop(context.Background()) })

	nb := netbiossvc.NewService(nil, nbServerName)
	nb.SetSessionConsumer(nbSessionBridge{adapter: smbsvc.ConsumerAdapter{Service: sm}})
	return sm, nb
}

// driverSMBNBIPX stands up the SMB-over-NBIPX server on a real IPX port bound to dev and
// dials it with the real client over the same dev, returning the connected ForkFS.
func driverSMBNBIPX(t *testing.T, dev string) fs.ForkFS {
	t.Helper()
	_, nb := driverSMBService(t)

	sec := &corenb.Section{SKey: ipxport.Name, IsEnabled: true}
	p, err := ipxport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) { return rawFrameLink(dev) },
		driverServerMAC, log.New(ipxport.Name))
	if err != nil {
		t.Fatalf("ipx NewInstanceFromOpener: %v", err)
	}
	rtr := ipxrouter.NewRouter(nil)
	rtr.SetIdentity(ipxrouter.DefaultNetwork, driverServerMAC)
	rtr.AddPort(p.(ipxrouter.Port))
	eng := nb.NewIPXEngine(rtr)
	for _, sock := range [][2]byte{
		netbiossvc.NBIPXSessionSocket, netbiossvc.NBIPXNameQuerySocket,
		netbiossvc.NBIPXDatagramSocket, netbiossvc.NBIPXNameSocket,
	} {
		if err := rtr.RegisterSocket(sock, eng); err != nil {
			t.Fatalf("RegisterSocket(%v): %v", sock, err)
		}
	}
	startPort(t, p)
	startNetBIOS(t, nb)

	cl, err := rawFrameLink(dev)
	if err != nil {
		t.Fatalf("client rawFrameLink: %v", err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	tr, err := clientsmb.DialNBIPX(cl, clientsmb.RandomMAC(), nbServerName)
	if err != nil {
		t.Fatalf("DialNBIPX over wire: %v", err)
	}
	return openSMBWire(t, tr)
}

// driverSMBNBF stands up the SMB-over-NBF server on a real NetBEUI port (LLC2 responder)
// bound to dev and dials it with the real client over the same dev.
func driverSMBNBF(t *testing.T, dev string) fs.ForkFS {
	t.Helper()
	_, nb := driverSMBService(t)

	sec := &corenb.Section{SKey: nbfport.Name, IsEnabled: true}
	p, err := nbfport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) { return rawFrameLink(dev) },
		driverServerMACnbf, log.New(nbfport.Name))
	if err != nil {
		t.Fatalf("netbeui NewInstanceFromOpener: %v", err)
	}
	rtr := netbeuirouter.NewRouter(nil)
	rtr.AddPort(p.(netbeuirouter.Port))
	eng := nb.NewNBFEngine(rtr)
	for _, n := range nb.LocalNames() {
		if err := rtr.RegisterName(n, eng); err != nil {
			t.Fatalf("RegisterName(%v): %v", n, err)
		}
	}
	if err := rtr.RegisterSession(eng); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if err := rtr.RegisterBroadcast(eng); err != nil {
		t.Fatalf("RegisterBroadcast: %v", err)
	}
	startPort(t, p)
	startNetBIOS(t, nb)

	cl, err := rawFrameLink(dev)
	if err != nil {
		t.Fatalf("client rawFrameLink: %v", err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	tr, err := clientsmb.DialNBF(cl, clientsmb.RandomMAC(), nbServerName)
	if err != nil {
		t.Fatalf("DialNBF over wire: %v", err)
	}
	return openSMBWire(t, tr)
}

// openSMBWire runs the SMB session over a wire transport and wraps the base FS the same way
// the SDK does. (Distinct from openSMB in servers_test.go, which is !driverint-tagged.)
func openSMBWire(t *testing.T, tr clientsmb.Transport) fs.ForkFS {
	t.Helper()
	sess, err := clientsmb.Open(tr, clientsmb.DialParams{ServerName: nbServerName, Share: "Share"})
	if err != nil {
		t.Fatalf("smb.Open over wire: %v", err)
	}
	store, err := metastore.Open("mem", "")
	if err != nil {
		t.Fatalf("metastore.Open: %v", err)
	}
	remote, err := fs.WrapBase(clientsmb.New(sess), fs.ShareSpec{
		Name: "Share", ForkBackend: "appledouble", FilenameCodec: "identity",
	}, store)
	if err != nil {
		t.Fatalf("WrapBase: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}
