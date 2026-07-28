package e2e

// servers_test.go builds one REAL in-process server per protocol×transport and returns a
// connected client fs.ForkFS. Each builder mirrors the wiring in the matching
// client/*/e2e_test.go (the focused per-transport gates), so this consolidated harness
// stays faithful to how the runtime composes each stack — it does not stub any engine.
//
// The transports covered here are exactly those that run without a physical NIC / kernel
// driver: AFP over a DDP bridge (transport-agnostic — models LToUDP and EtherTalk, which
// differ only in the port link below DDP), SMB over direct-IPX / NBIPX / NBF / TCP-NBT,
// NCP over IPX, and EtherDFS over a raw-Ethernet inmem pair. NBIPX/NBF/EtherDFS drive the
// GENUINE core ports (LLC2, IPX/NBIPX session engines) over an in-memory Ethernet pair.

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	clientpkg "github.com/ObsoleteMadness/ClassicStack/client"
	_ "github.com/ObsoleteMadness/ClassicStack/client/afp" // register the afp scheme
	clientetherdfs "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	clientncp "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	corenb "github.com/ObsoleteMadness/ClassicStack/core/port"
	etherport "github.com/ObsoleteMadness/ClassicStack/core/port/etherdfs"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	nbfport "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	ddp "github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	ipxrouter "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"
	netbeuirouter "github.com/ObsoleteMadness/ClassicStack/core/router/netbeui"
	afpsvc "github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	etherdfssvc "github.com/ObsoleteMadness/ClassicStack/core/service/etherdfs"
	ncpsvc "github.com/ObsoleteMadness/ClassicStack/core/service/ncp"
	netbiossvc "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	smbsvc "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// nbServerName is the NetBIOS name the SMB server claims; the NBF/NBIPX clients call it.
const nbServerName = "CLASSICSTACK"

// memShare is the ShareSpec every server exposes: an in-memory volume with the AppleDouble
// fork backend (so SMB/NCP/EtherDFS, which have no native fork, still carry resource forks
// and Finder info as sidecars) and an identity filename codec.
func memShare(name string) fs.ShareSpec {
	return fs.ShareSpec{
		Name: name, FSType: "memfs",
		ForkBackend: "appledouble", FilenameCodec: "identity",
	}
}

// wrapClientFS applies the same fork/meta stack client.Connect layers over a base client
// FS, so a directly-dialled transport gets the identical ForkFS the SDK would build.
func wrapClientFS(t *testing.T, base fs.FileSystem, share string) fs.ForkFS {
	t.Helper()
	store, err := metastore.Open("mem", "")
	if err != nil {
		t.Fatalf("metastore.Open: %v", err)
	}
	remote, err := fs.WrapBase(base, fs.ShareSpec{
		Name: share, ForkBackend: "appledouble", FilenameCodec: "identity",
	}, store)
	if err != nil {
		t.Fatalf("WrapBase: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}

// ---------------------------------------------------------------------------
// AFP over DDP (models LToUDP / EtherTalk — the DDP payload is transport-agnostic)
// ---------------------------------------------------------------------------

// ddpBridge is both the client's DDP DatagramLink and the server's ServiceRouter — one
// point-to-point DDP link, as in client/afp/e2e_test.go.
type ddpBridge struct {
	svc      *afpsvc.Service
	clientRx chan ddp.Datagram
	mu       sync.Mutex
	closed   bool
}

func (b *ddpBridge) WriteDatagram(d ddp.Datagram) error { b.svc.Inbound(d, ddpFakePort{}); return nil }

func (b *ddpBridge) ReadDatagram() (ddp.Datagram, error) {
	d, ok := <-b.clientRx
	if !ok {
		return ddp.Datagram{}, io.EOF
	}
	return d, nil
}

func (b *ddpBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.clientRx)
	}
	return nil
}

func (b *ddpBridge) Reply(d ddp.Datagram, _ router.RoutedPort, ddpType uint8, data []byte) {
	b.deliver(ddp.Datagram{
		DestNetwork: d.SrcNetwork, SrcNetwork: d.DestNetwork,
		DestNode: d.SrcNode, SrcNode: d.DestNode,
		DestSocket: d.SrcSocket, SrcSocket: d.DestSocket,
		DDPType: ddpType, Data: append([]byte(nil), data...),
	})
}

func (b *ddpBridge) Route(d ddp.Datagram, _ bool) error {
	d.Data = append([]byte(nil), d.Data...)
	b.deliver(d)
	return nil
}

func (b *ddpBridge) deliver(d ddp.Datagram) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	select {
	case b.clientRx <- d:
	default:
	}
}

func (b *ddpBridge) RoutingTable() *router.RoutingTable  { return nil }
func (b *ddpBridge) Zones() *router.ZoneInformationTable { return nil }
func (b *ddpBridge) Ports() []router.RoutedPort          { return nil }

type ddpFakePort struct{ router.RoutedPort }

// afpServer builds a running AFP service (memfs "Share") behind a DDP bridge and returns
// the connected client via the PUBLIC client.Connect path (afp://net.node/Share).
func afpServer(t *testing.T) fs.ForkFS {
	t.Helper()
	svc, err := afpsvc.NewWithVolumes(nil, afpsvc.VolumeSpec{
		ID: 1, Name: "Share", Share: memShare("Share"),
	})
	if err != nil {
		t.Fatalf("afp NewWithVolumes: %v", err)
	}
	br := &ddpBridge{svc: svc, clientRx: make(chan ddp.Datagram, 64)}
	svc.SetRouter(br)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("afp Start: %v", err)
	}
	t.Cleanup(func() { _ = br.Close() })

	target, err := uri.Parse("afp://0.0/Share")
	if err != nil {
		t.Fatalf("uri.Parse: %v", err)
	}
	remote, err := clientpkg.Connect(context.Background(), target, clientpkg.Options{
		Opener: clientlink.NewDatagramOpener(br),
	})
	if err != nil {
		t.Fatalf("client.Connect afp: %v", err)
	}
	t.Cleanup(func() { _ = fs.CloseFS(remote) })
	return remote
}

// ---------------------------------------------------------------------------
// SMB over a direct in-process circuit bridge (models the message-level transport)
// ---------------------------------------------------------------------------

type smbBridge struct{ conn smbsvc.SessionCircuit }

func (b *smbBridge) Send(req []byte) ([]byte, error) { return b.conn.ServeMessage(req), nil }
func (b *smbBridge) MaxResponse() int                { return 1 << 20 }
func (b *smbBridge) Close() error                    { b.conn.Close(); return nil }

// smbBridgeServer builds a running SMB service (memfs "Share") and dials it over an
// in-process circuit — the message-level SMB path (client/smb/e2e_test.go).
func smbBridgeServer(t *testing.T) fs.ForkFS {
	t.Helper()
	svc := newSMBService(t)
	conn := svc.NewConn("e2e")
	t.Cleanup(func() { conn.Close() })
	return openSMB(t, &smbBridge{conn: conn})
}

// newSMBService builds and starts an SMB service with one memfs "Share".
func newSMBService(t *testing.T) *smbsvc.Service {
	t.Helper()
	svc, err := smbsvc.NewWithShares(nil, smbsvc.ShareSpec{Name: "Share", Share: memShare("Share")})
	if err != nil {
		t.Fatalf("smb NewWithShares: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("smb Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	return svc
}

// openSMB runs the SMB session over a client transport and wraps the base FS.
func openSMB(t *testing.T, tr clientsmb.Transport) fs.ForkFS {
	t.Helper()
	sess, err := clientsmb.Open(tr, clientsmb.DialParams{ServerName: nbServerName, Share: "Share"})
	if err != nil {
		t.Fatalf("smb.Open: %v", err)
	}
	return wrapClientFS(t, clientsmb.New(sess), "Share")
}

// ---------------------------------------------------------------------------
// SMB over TCP/NBT (real client tcpTransport framing over a net.Pipe)
// ---------------------------------------------------------------------------

// smbTCPServer dials the SMB client's real TCP/NBT transport over a net.Pipe whose server
// end runs a minimal NBT frame pump (24-bit length prefix) feeding Conn.ServeMessage — so
// the client's session-message framing is exercised against the genuine SMB circuit.
func smbTCPServer(t *testing.T) fs.ForkFS {
	t.Helper()
	svc := newSMBService(t)
	clientConn, serverConn := net.Pipe()
	conn := svc.NewConn("tcp-e2e")
	t.Cleanup(func() { conn.Close(); _ = serverConn.Close(); _ = clientConn.Close() })

	go serveNBT(serverConn, conn)
	return openSMB(t, clientsmb.DialTCP(clientConn))
}

// serveNBT reads NBT session messages (msg-type byte + 24-bit big-endian length + body)
// off c, dispatches each to conn.ServeMessage, and writes the framed reply back — the
// server half of the direct-TCP/NBT framing.
func serveNBT(c net.Conn, conn smbsvc.SessionCircuit) {
	var hdr [4]byte
	for {
		if _, err := io.ReadFull(c, hdr[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(hdr[:]) & 0x00FFFFFF
		req := make([]byte, n)
		if _, err := io.ReadFull(c, req); err != nil {
			return
		}
		resp := conn.ServeMessage(req)
		var rh [4]byte
		binary.BigEndian.PutUint32(rh[:], uint32(len(resp)))
		rh[0] = 0x00
		if _, err := c.Write(rh[:]); err != nil {
			return
		}
		if _, err := c.Write(resp); err != nil {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// SMB over NBIPX (real IPX port + NBIPX session engine over an inmem pair)
// ---------------------------------------------------------------------------

var nbipxServerMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x5B}

func smbNBIPXServer(t *testing.T) fs.ForkFS {
	t.Helper()
	serverEnd, clientEnd := inmem.Pair(64)
	sm := newSMBService(t)

	nb := netbiossvc.NewService(nil, nbServerName)
	nb.SetSessionConsumer(nbSessionBridge{adapter: smbsvc.ConsumerAdapter{Service: sm}})

	sec := &corenb.Section{SKey: ipxport.Name, IsEnabled: true}
	p, err := ipxport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) { return serverEnd, nil },
		nbipxServerMAC, log.New(ipxport.Name))
	if err != nil {
		t.Fatalf("ipx NewInstanceFromOpener: %v", err)
	}
	rtr := ipxrouter.NewRouter(nil)
	rtr.SetIdentity(ipxrouter.DefaultNetwork, nbipxServerMAC)
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

	tr, err := clientsmb.DialNBIPX(clientEnd, clientsmb.RandomMAC(), nbServerName)
	if err != nil {
		t.Fatalf("DialNBIPX: %v", err)
	}
	return openSMB(t, tr)
}

// ---------------------------------------------------------------------------
// SMB over NBF (real NetBEUI port + LLC2 responder + NBF session engine over an inmem pair)
// ---------------------------------------------------------------------------

var nbfServerMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xBF}

func smbNBFServer(t *testing.T) fs.ForkFS {
	t.Helper()
	serverEnd, clientEnd := inmem.Pair(64)
	sm := newSMBService(t)

	nb := netbiossvc.NewService(nil, nbServerName)
	nb.SetSessionConsumer(nbSessionBridge{adapter: smbsvc.ConsumerAdapter{Service: sm}})

	sec := &corenb.Section{SKey: nbfport.Name, IsEnabled: true}
	p, err := nbfport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) { return serverEnd, nil },
		nbfServerMAC, log.New(nbfport.Name))
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

	tr, err := clientsmb.DialNBF(clientEnd, clientsmb.RandomMAC(), nbServerName)
	if err != nil {
		t.Fatalf("DialNBF: %v", err)
	}
	return openSMB(t, tr)
}

// ---------------------------------------------------------------------------
// NCP over IPX (in-process IPX-datagram bridge)
// ---------------------------------------------------------------------------

var (
	ncpClientNode = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	ncpServerNode = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xFE}
	ncpSock       = [2]byte{0x04, 0x51}
)

type ncpBridge struct {
	over  *ncpsvc.OverIPX
	mu    sync.Mutex
	reply []byte
}

func (b *ncpBridge) Send(req []byte) ([]byte, error) {
	b.mu.Lock()
	b.reply = nil
	b.mu.Unlock()
	b.over.HandleDatagram(&ipxproto.Datagram{
		Type: 0x11, SrcNode: ncpClientNode, SrcSock: ncpSock,
		DstNode: ncpServerNode, DstSock: ncpSock, Payload: req,
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reply, nil
}

func (b *ncpBridge) MaxPayload() int { return 1024 }
func (b *ncpBridge) Close() error    { return nil }

type ncpCaptureSender struct{ b *ncpBridge }

func (s ncpCaptureSender) Send(d *ipxproto.Datagram) error {
	s.b.mu.Lock()
	s.b.reply = append([]byte(nil), d.Payload...)
	s.b.mu.Unlock()
	return nil
}

// ncpServer builds a running NCP service (memfs "SYS") behind an IPX bridge and returns
// the connected client (client/ncp/e2e_test.go).
func ncpServer(t *testing.T) fs.ForkFS {
	t.Helper()
	svc := ncpsvc.New(nil)
	if err := svc.AddShare(memShare("SYS")); err != nil {
		t.Fatalf("ncp AddShare: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("ncp Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	b := &ncpBridge{}
	b.over = svc.NewOverIPX(ncpCaptureSender{b})
	sess, err := clientncp.Open(b, clientncp.DialParams{Volume: "SYS"})
	if err != nil {
		t.Fatalf("ncp.Open: %v", err)
	}
	return wrapClientFS(t, clientncp.New(sess), "SYS")
}

// ---------------------------------------------------------------------------
// EtherDFS over a raw-Ethernet inmem pair (real port read loop + service dispatch)
// ---------------------------------------------------------------------------

var etherdfsServerMAC = [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xED}

func etherdfsServer(t *testing.T) fs.ForkFS {
	t.Helper()
	serverEnd, clientEnd := inmem.Pair(64)

	sec := &corenb.Section{SKey: etherport.Name, IsEnabled: true}
	p, err := etherport.NewInstanceFromOpener(sec, func() (link.FrameLink, error) { return serverEnd, nil },
		etherdfsServerMAC, log.New(etherport.Name))
	if err != nil {
		t.Fatalf("etherdfs NewInstanceFromOpener: %v", err)
	}
	svc := etherdfssvc.New(p, log.New(etherdfssvc.Name))
	drive := memShare("C")
	drive.FilenameCodec = "identity"
	if err := svc.ReconcileDrives([]etherdfssvc.DriveSpec{{Name: "C", Share: drive}}); err != nil {
		t.Fatalf("ReconcileDrives: %v", err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("etherdfs Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	tr := clientetherdfs.DialFrame(clientEnd, clientetherdfs.RandomMAC())
	sess, err := clientetherdfs.Open(tr, clientetherdfs.DialParams{Drive: "C"})
	if err != nil {
		t.Fatalf("etherdfs.Open: %v", err)
	}
	return wrapClientFS(t, clientetherdfs.New(sess), "C")
}

// ---------------------------------------------------------------------------
// shared helpers for the real-port stacks
// ---------------------------------------------------------------------------

// startPort starts a core port's read loop and registers its Stop for cleanup.
func startPort(t *testing.T, p any) {
	t.Helper()
	if err := p.(interface{ Start(context.Context) error }).Start(context.Background()); err != nil {
		t.Fatalf("port Start: %v", err)
	}
	t.Cleanup(func() { _ = p.(interface{ Stop(context.Context) error }).Stop(context.Background()) })
}

func startNetBIOS(t *testing.T, nb *netbiossvc.Service) {
	t.Helper()
	if err := nb.Start(context.Background()); err != nil {
		t.Fatalf("netbios Start: %v", err)
	}
	t.Cleanup(func() { _ = nb.Stop(context.Background()) })
}

// nbSessionBridge adapts an smb.SessionConsumer to a netbios.SessionConsumer (structurally
// identical, distinct types), mirroring compose's smbSessionBridge.
type nbSessionBridge struct{ adapter smbsvc.SessionConsumer }

func (b nbSessionBridge) NewConn(client string) netbiossvc.SessionCircuit {
	return nbCircuitBridge{c: b.adapter.NewConn(client)}
}

type nbCircuitBridge struct{ c smbsvc.SessionCircuit }

func (b nbCircuitBridge) ServeMessage(req []byte) []byte { return b.c.ServeMessage(req) }
func (b nbCircuitBridge) SetPushWriter(w func([]byte))   { b.c.SetPushWriter(w) }
func (b nbCircuitBridge) Close()                         { b.c.Close() }
func (b nbCircuitBridge) SetNetBIOSName(name string) {
	if namer, ok := b.c.(netbiossvc.NetBIOSNamer); ok {
		namer.SetNetBIOSName(name)
	}
}
