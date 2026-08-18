package afp

import (
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"

	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
)

// DSIPort is the well-known AFP-over-TCP (DSI) port. Servers advertise it via
// Bonjour as _afpovertcp._tcp.local. (RFC 6762 / Apple AFP over TCP).
const DSIPort = 548

const (
	mdnsGroup       = "224.0.0.251"
	mdnsPort        = 5353
	afpOverTCPSvc   = "_afpovertcp._tcp.local."
	classQU         = 0x8000 // RFC 6762 unicast-response bit on a question class
	mdnsDefaultWait = 2 * time.Second
)

// TCPServer is one AFP-over-TCP server learned from mDNS (Bonjour), not from NBP.
// Host is an IPv4 when the response carried an A record, otherwise the SRV target
// hostname. Port is 548 when the advertisement omitted SRV.
type TCPServer struct {
	Name string // instance label (the Chooser-style server name)
	Host string // IPv4 or hostname to dial
	Port uint16
}

// DiscoverTCP browses _afpovertcp._tcp.local via multicast DNS from device's IPv4
// (or the wildcard address when device has none). Replies are requested unicast
// (QU) so this client does not need to bind UDP 5353, which mDNSResponder owns on
// macOS. A quiet segment yields an empty list, not a fatal error.
func DiscoverTCP(device string, window time.Duration) ([]TCPServer, error) {
	src := ipv4ForDevice(device)
	return discoverTCP(src, window)
}

func discoverTCP(src net.IP, window time.Duration) ([]TCPServer, error) {
	if window <= 0 {
		window = mdnsDefaultWait
	}
	query, err := packAFPMDNSQuery()
	if err != nil {
		return nil, err
	}
	laddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	if src != nil {
		laddr.IP = src
	}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, fmt.Errorf("afp: mDNS listen: %w", err)
	}
	defer conn.Close()

	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetTTL(255)
	_ = pc.SetMulticastTTL(255)
	if src != nil {
		if ifi := interfaceForIP(src); ifi != nil {
			_ = pc.SetMulticastInterface(ifi)
		}
	}

	dst := &net.UDPAddr{IP: net.ParseIP(mdnsGroup), Port: mdnsPort}
	if _, err := conn.WriteToUDP(query, dst); err != nil {
		return nil, fmt.Errorf("afp: mDNS query: %w", err)
	}

	acc := newMDNSAccum()
	_ = conn.SetReadDeadline(time.Now().Add(window))
	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			if acc.empty() {
				return nil, fmt.Errorf("afp: mDNS read: %w", err)
			}
			break
		}
		acc.add(buf[:n])
	}
	return acc.servers(), nil
}

func packAFPMDNSQuery() ([]byte, error) {
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 0},
		Questions: []dnsmessage.Question{{
			Name:  dnsmessage.MustNewName(afpOverTCPSvc),
			Type:  dnsmessage.TypePTR,
			Class: dnsmessage.Class(uint16(dnsmessage.ClassINET) | classQU),
		}},
	}
	return msg.Pack()
}

type mdnsAccum struct {
	// ptrs maps a lowercased instance FQDN to the original-case name (from PTR
	// or SRV), so the Chooser-style label keeps the advertised capitalisation.
	ptrs  map[string]string
	srvs  map[string]dnsmessage.SRVResource
	addrs map[string]net.IP
}

func newMDNSAccum() *mdnsAccum {
	return &mdnsAccum{
		ptrs:  map[string]string{},
		srvs:  map[string]dnsmessage.SRVResource{},
		addrs: map[string]net.IP{},
	}
}

func (a *mdnsAccum) empty() bool {
	return len(a.ptrs) == 0 && len(a.srvs) == 0
}

func (a *mdnsAccum) noteInstance(orig string) string {
	orig = strings.TrimSuffix(orig, ".")
	key := strings.ToLower(orig)
	if key == "" {
		return ""
	}
	if _, ok := a.ptrs[key]; !ok {
		a.ptrs[key] = orig
	}
	return key
}

func (a *mdnsAccum) add(buf []byte) {
	var p dnsmessage.Parser
	if _, err := p.Start(buf); err != nil {
		return
	}
	if err := p.SkipAllQuestions(); err != nil {
		return
	}
	answers, err := p.AllAnswers()
	if err != nil {
		return
	}
	_ = p.SkipAllAuthorities()
	adds, _ := p.AllAdditionals()
	for _, r := range append(answers, adds...) {
		a.consume(r)
	}
}

func (a *mdnsAccum) consume(r dnsmessage.Resource) {
	if r.Body == nil {
		return
	}
	owner := dnsName(r.Header.Name)
	switch b := r.Body.(type) {
	case *dnsmessage.PTRResource:
		if owner != dnsName(dnsmessage.MustNewName(afpOverTCPSvc)) {
			return
		}
		a.noteInstance(b.PTR.String())
	case *dnsmessage.SRVResource:
		if !isAFPOverTCPInstance(owner) {
			return
		}
		key := a.noteInstance(r.Header.Name.String())
		a.srvs[key] = *b
	case *dnsmessage.AResource:
		ip := net.IP(b.A[:]).To4()
		if ip != nil && owner != "" {
			a.addrs[owner] = ip
		}
	}
}

func (a *mdnsAccum) servers() []TCPServer {
	out := make([]TCPServer, 0, len(a.ptrs))
	for key, orig := range a.ptrs {
		srv := TCPServer{
			Name: mdnsInstanceLabel(orig),
			Port: DSIPort,
		}
		if rec, ok := a.srvs[key]; ok {
			if rec.Port != 0 {
				srv.Port = rec.Port
			}
			target := dnsName(rec.Target)
			if ip := a.addrs[target]; ip != nil {
				srv.Host = ip.String()
			} else if target != "" {
				srv.Host = strings.TrimSuffix(rec.Target.String(), ".")
			}
		}
		if srv.Host == "" {
			if ip := a.addrs[key]; ip != nil {
				srv.Host = ip.String()
			} else if srv.Name != "" {
				srv.Host = srv.Name + ".local"
			}
		}
		if srv.Name == "" && srv.Host == "" {
			continue
		}
		if srv.Name == "" {
			srv.Name = srv.Host
		}
		out = append(out, srv)
	}
	return out
}

func dnsName(n dnsmessage.Name) string {
	return strings.ToLower(strings.TrimSuffix(n.String(), "."))
}

func isAFPOverTCPInstance(name string) bool {
	return strings.HasSuffix(name, "._afpovertcp._tcp.local") || name == "_afpovertcp._tcp.local"
}

func mdnsInstanceLabel(fqdn string) string {
	const suffix = "._afpovertcp._tcp.local"
	s := strings.TrimSuffix(fqdn, ".")
	if i := strings.Index(strings.ToLower(s), suffix); i > 0 {
		return s[:i]
	}
	return s
}

func ipv4ForDevice(device string) net.IP {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil
	}
	if devs, err := clientlink.ListInterfaces(); err == nil {
		for _, d := range devs {
			if !strings.EqualFold(d.Name, device) {
				continue
			}
			if ip := firstIPv4(d.Addresses); ip != nil {
				return ip
			}
		}
	}
	ifi, err := net.InterfaceByName(device)
	if err != nil {
		return nil
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	var dotted []string
	for _, a := range addrs {
		dotted = append(dotted, a.String())
	}
	return firstIPv4(dotted)
}

func firstIPv4(addrs []string) net.IP {
	for _, a := range addrs {
		s, _, _ := strings.Cut(a, "/")
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() {
			return ip4
		}
	}
	return nil
}

func interfaceForIP(ip net.IP) *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var cand net.IP
			switch v := a.(type) {
			case *net.IPNet:
				cand = v.IP
			case *net.IPAddr:
				cand = v.IP
			}
			if cand != nil && cand.Equal(ip) {
				return &ifaces[i]
			}
		}
	}
	return nil
}
