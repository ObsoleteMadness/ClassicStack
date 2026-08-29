//go:build !tinygo

// nbns.go is the NBT name-service (UDP 137) client used to find the TCP/IP master
// browser. A "net view" over TCP/IP locates <workgroup><1D> via a broadcast NBNS
// query, then asks that host for the browse list over SMB-over-TCP. This is the
// datagram half; client/browse.EnumerateTCP runs the session half.
//
// It needs a real UDP socket (net.ListenUDP), which TinyGo's baremetal targets
// don't implement -- see nbns_tinygo.go for the stub those targets get instead.

package netbios

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

const (
	nbnsPort        = 137
	nbnsTypeNB      = 0x0020 // NB resource record (RFC 1002)
	nbnsClassIN     = 0x0001
	nbnsFlagsQuery  = 0x0110 // query, recursion desired, broadcast
	nbnsHeaderLen   = 12
	nbnsRDataLen    = 6 // NB flags (2) + IPv4 (4)
	nbnsEncodedName = 34
)

// LookupMasterBrowser broadcasts an NBNS query for <workgroup><1D> from src (the
// IPv4 bound on the browse NIC) and returns the IPv4s that answered within window.
// workgroup empty uses WORKGROUP, the same blind default the NBF/NBIPX FindMaster path
// uses. It never returns a fatal error for a quiet segment — an empty slice is a miss.
func LookupMasterBrowser(src net.IP, workgroup string, window time.Duration) ([]NBNSAnswer, error) {
	workgroup = strings.ToUpper(strings.TrimSpace(workgroup))
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	name := nb.NewName(workgroup, browserproto.NameTypeMasterBrowser)
	return nbnsQuery(src, name, window)
}

func nbnsQuery(src net.IP, name nb.Name, window time.Duration) ([]NBNSAnswer, error) {
	if src == nil || src.To4() == nil {
		return nil, fmt.Errorf("netbios: NBNS query needs an IPv4 source")
	}
	if window <= 0 {
		window = 2 * time.Second
	}
	src4 := src.To4()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: src4, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("netbios: listen NBNS: %w", err)
	}
	defer func() { _ = pc.Close() }()
	if err := setBroadcast(pc); err != nil {
		dtracef("NBNS SO_BROADCAST: %v", err)
	}

	id := uint16(time.Now().UnixNano())
	query := marshalNBNSQuery(id, name)
	dst := &net.UDPAddr{IP: net.IPv4bcast, Port: nbnsPort}
	dtracef("NBNS query %q <%02x> from %s", name.String(), name.Type(), src4)
	if _, err := pc.WriteToUDP(query, dst); err != nil {
		return nil, fmt.Errorf("netbios: send NBNS query: %w", err)
	}

	_ = pc.SetReadDeadline(time.Now().Add(window))
	seen := map[string]NBNSAnswer{}
	buf := make([]byte, 512)
	for {
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) {
				break
			}
			if len(seen) > 0 {
				break
			}
			return nil, err
		}
		for _, a := range parseNBNSAnswers(buf[:n], id) {
			if a.IP == nil {
				if ip := addr.IP.To4(); ip != nil {
					a.IP = ip
				}
			}
			if a.IP == nil {
				continue
			}
			key := a.IP.String()
			if _, ok := seen[key]; ok {
				continue
			}
			if a.Name == "" {
				a.Name = name.String()
			}
			seen[key] = a
			dtracef("NBNS %s → %s", a.Name, a.IP)
		}
	}
	out := make([]NBNSAnswer, 0, len(seen))
	for _, a := range seen {
		out = append(out, a)
	}
	return out, nil
}

func marshalNBNSQuery(id uint16, name nb.Name) []byte {
	out := make([]byte, nbnsHeaderLen+nbnsEncodedName+4)
	binary.BigEndian.PutUint16(out[0:2], id)
	binary.BigEndian.PutUint16(out[2:4], nbnsFlagsQuery)
	binary.BigEndian.PutUint16(out[4:6], 1) // QDCOUNT
	copy(out[nbnsHeaderLen:], encodeNBNSName(name))
	off := nbnsHeaderLen + nbnsEncodedName
	binary.BigEndian.PutUint16(out[off:off+2], nbnsTypeNB)
	binary.BigEndian.PutUint16(out[off+2:off+4], nbnsClassIN)
	return out
}

// encodeNBNSName is RFC 1002 first-level encoding of a 16-byte NetBIOS name as a
// 32-byte A–P label plus a zero root label (34 bytes total).
func encodeNBNSName(name nb.Name) []byte {
	out := make([]byte, nbnsEncodedName)
	out[0] = 32
	for i, b := range name {
		out[1+2*i] = 'A' + (b >> 4)
		out[1+2*i+1] = 'A' + (b & 0x0F)
	}
	return out
}

func parseNBNSAnswers(pkt []byte, id uint16) []NBNSAnswer {
	if len(pkt) < nbnsHeaderLen {
		return nil
	}
	if binary.BigEndian.Uint16(pkt[0:2]) != id {
		return nil
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	if flags&0x8000 == 0 {
		return nil // not a response
	}
	qd := int(binary.BigEndian.Uint16(pkt[4:6]))
	an := int(binary.BigEndian.Uint16(pkt[6:8]))
	off := nbnsHeaderLen
	for i := 0; i < qd && off < len(pkt); i++ {
		off = skipNBNSName(pkt, off)
		off += 4 // type + class
	}
	var out []NBNSAnswer
	for i := 0; i < an && off < len(pkt); i++ {
		nameOff := off
		off = skipNBNSName(pkt, off)
		if off+10 > len(pkt) {
			break
		}
		rrType := binary.BigEndian.Uint16(pkt[off : off+2])
		rdlen := int(binary.BigEndian.Uint16(pkt[off+8 : off+10]))
		off += 10
		if off+rdlen > len(pkt) {
			break
		}
		if rrType == nbnsTypeNB && rdlen >= nbnsRDataLen {
			ip := net.IPv4(pkt[off+2], pkt[off+3], pkt[off+4], pkt[off+5]).To4()
			out = append(out, NBNSAnswer{Name: decodeNBNSName(pkt, nameOff), IP: ip})
		}
		off += rdlen
	}
	return out
}

func skipNBNSName(pkt []byte, off int) int {
	for off < len(pkt) {
		l := int(pkt[off])
		if l == 0 {
			return off + 1
		}
		if l&0xC0 == 0xC0 {
			return off + 2 // compression pointer
		}
		off += 1 + l
	}
	return len(pkt)
}

func decodeNBNSName(pkt []byte, off int) string {
	if off >= len(pkt) {
		return ""
	}
	if pkt[off]&0xC0 == 0xC0 {
		ptr := int(binary.BigEndian.Uint16(pkt[off:off+2]) & 0x3FFF)
		if ptr >= len(pkt) {
			return ""
		}
		return decodeNBNSName(pkt, ptr)
	}
	l := int(pkt[off])
	if l != 32 || off+1+32 > len(pkt) {
		return ""
	}
	var name nb.Name
	for i := 0; i < 16; i++ {
		hi := pkt[off+1+2*i]
		lo := pkt[off+1+2*i+1]
		if hi < 'A' || lo < 'A' {
			return ""
		}
		name[i] = ((hi - 'A') << 4) | (lo - 'A')
	}
	return name.String()
}

func setBroadcast(pc *net.UDPConn) error {
	raw, err := pc.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = setBroadcastFD(fd)
	}); err != nil {
		return err
	}
	return serr
}
