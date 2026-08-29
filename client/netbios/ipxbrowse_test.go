package netbios

import (
	"encoding/hex"
	"strings"
	"testing"

	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// ipxbrowse_test.go pins the NWLink browser datagram plane (socket 0x0553) against the
// golden captures. Both IPX carriers ride it verbatim — NBIPX and direct-hosted IPX differ
// only in the SMB session leg that follows — so every case here runs over both.

// goldenNMPIBrowseHeader is the NMPI fixed header of golden
// spec/captures/nbipx-win98.pcap frame 58 (WIN98-2's broadcast GetBackupList request),
// from the opcode byte through the source name: opcode 0xFC MailslotSend, name type 0x02
// NMPINameTypeWorkgroup, message id 0, RequestedName "WORKGROUP      "<00>, SourceName
// "WIN98-2        "<00>. nwlink-win98.pcap frames 26-40 carry the identical 20 bytes
// ahead of the source name, which is why the two carriers share one expectation.
const goldenNMPIBrowseHeader = "fc020000" +
	"574f524b47524f555020202020202000" // WORKGROUP<00>

// TestIPXBrowseDatagramsMatchGolden proves both browser datagrams this client emits on the
// NWLink plane — the AnnouncementRequest that solicits a re-announce and the GetBackupList
// that names the master — carry golden's NMPI addressing: IPX type 20 on socket 0x0553,
// opcode 0xFC, name type NMPINameTypeWorkgroup, addressed to <workgroup><00>.
//
// The pre-fix client addressed them to "*"<1E> and <workgroup><1D> with name type
// NMPINameTypeMachine — names golden never puts on this socket. Four live NBIPX stations
// answered none of them, so an NBIPX browse always came back empty.
func TestIPXBrowseDatagramsMatchGolden(t *testing.T) {
	t.Parallel()
	want, err := hex.DecodeString(goldenNMPIBrowseHeader)
	if err != nil {
		t.Fatalf("golden header: %v", err)
	}
	for _, proto := range []Protocol{NBIPX, IPX} {
		for _, tc := range []struct {
			name string
			send func(*Conn) error
			op   uint8
		}{
			{"AnnouncementRequest", func(c *Conn) error { return c.solicit("WORKGROUP") }, browserproto.OpAnnouncementRequest},
			{"GetBackupList", func(c *Conn) error { return c.requestBackupList("WORKGROUP") }, browserproto.OpGetBackupListReq},
		} {
			t.Run(string(proto)+"/"+tc.name, func(t *testing.T) {
				c := &Conn{proto: proto, srcMAC: RandomMAC(), srcName: nb.NewName("CS-TEST", NameTypeWorkstation)}
				captured := &captureLink{}
				c.fl = captured
				if err := tc.send(c); err != nil {
					t.Fatalf("send: %v", err)
				}
				d, err := ipxproto.Decode(captured.last[ethHdrLen:])
				if err != nil {
					t.Fatalf("ipx decode: %v", err)
				}
				if d.Type != ipxNetBIOSTyp {
					t.Errorf("IPX type = %#x, want %#x (fan-out browser datagrams are type 20)", d.Type, ipxNetBIOSTyp)
				}
				if d.SrcSock != nb.NBIPXDatagramSocket || d.DstSock != nb.NBIPXDatagramSocket {
					t.Errorf("sockets = %x→%x, want 0553→0553", d.SrcSock, d.DstSock)
				}
				// The NMPI header runs Routers(32) then opcode/type/id/requested-name.
				got := d.Payload[nb.NBIPXWANRouterBytes : nb.NBIPXWANRouterBytes+len(want)]
				if string(got) != string(want) {
					t.Errorf("NMPI header = % x,\n            want % x (golden nbipx-win98.pcap frame 58)", got, want)
				}
				// And it really is the browser opcode the step intends.
				nmpi, err := nb.DecodeNMPIPacket(d.Payload)
				if err != nil {
					t.Fatalf("DecodeNMPIPacket: %v", err)
				}
				w, err := mailslotproto.Unmarshal(nmpi.Payload)
				if err != nil || !strings.EqualFold(w.Name, mailslotproto.NameBrowse) {
					t.Fatalf("mailslot = %q err=%v, want %s", w.Name, err, mailslotproto.NameBrowse)
				}
				if op, _, ok := browserproto.UnwrapPayload(w.Body); !ok || op != tc.op {
					t.Fatalf("browser op = %#x ok=%t, want %#x", op, ok, tc.op)
				}
			})
		}
	}
}

// TestIPXSolicitMastersSkipsMSBrowse pins that the __MSBROWSE__ solicit is NOT emitted on
// the NWLink datagram plane. Golden NT 3.51 addresses __MSBROWSE__ over the NB-IPX SESSION
// socket 0x0455 as a bare directed datagram (spec/captures/nbipx-nt351-win98.pcap frame
// 54); no capture shows it as an NMPI MailslotSend on 0x0553, so sending one there would
// put a frame on the wire no real stack emits. NBF still sends it.
func TestIPXSolicitMastersSkipsMSBrowse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		proto Protocol
		want  bool
	}{{NBF, true}, {NBIPX, false}, {IPX, false}} {
		t.Run(string(tc.proto), func(t *testing.T) {
			c := &Conn{proto: tc.proto, srcMAC: RandomMAC(), srcName: nb.NewName("CS-TEST", NameTypeWorkstation)}
			all := &recordLink{}
			c.fl = all
			if err := c.solicitMasters("WORKGROUP"); err != nil {
				t.Fatalf("solicitMasters: %v", err)
			}
			if got := all.mentions(msBrowseName); got != tc.want {
				t.Fatalf("__MSBROWSE__ emitted = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestNBFBrowseNamesUnchanged guards the carrier that already worked: NBF must keep
// soliciting "*"<1E> and directing its GetBackupList at the master's registered
// <workgroup><1D> (captures/win98nbf-win31nbf.pcapng frames 25→26). The IPX naming fix
// must not leak into it.
func TestNBFBrowseNamesUnchanged(t *testing.T) {
	t.Parallel()
	c := &Conn{proto: NBF, srcMAC: RandomMAC(), srcName: nb.NewName("CS-TEST", NameTypeWorkstation)}
	if got := c.browseFanoutName("WORKGROUP"); got != browseGroupName {
		t.Errorf("NBF fan-out name = %q<%#x>, want the wildcard group name", got.String(), got.Type())
	}
	master, fanout := c.masterTarget("WORKGROUP")
	if fanout || master.String() != "WORKGROUP" || master.Type() != nameTypeLocalMaster {
		t.Errorf("NBF master target = %q<%#x> fanout=%t, want WORKGROUP<%#x> directed",
			master.String(), master.Type(), fanout, nameTypeLocalMaster)
	}
}

// goldenBackupListResponse is golden spec/captures/nbipx-win98.pcap frame 60 verbatim: the
// master WIN98-1's GetBackupList RESPONSE, unicast back to the station that asked. It is
// IPX packet type 0x04 (PEP), not the type 0x14 the request rode — a directed answer needs
// no NetBIOS broadcast forwarding. nwlink-win98.pcap frame 41 is the same shape.
const goldenBackupListResponse = "0086b0863ad50086b0ae296f8137ffff00c60004000000000086b0863ad50553" +
	"000000000086b0ae296f05534e39382d32000000000000000000000000000000" +
	"000000000000000000000000fc01000057494e39382d32202020202020202000" +
	"57494e39382d31202020202020202020ff534d42250000000000000000000000" +
	"000000000000000000000000000000001100000e000000000000000000000000" +
	"000000000000000e00560003000100010002001f005c4d41494c534c4f545c42" +
	"524f575345000a010100000057494e39382d3100"

// TestDecodeGoldenBackupListResponse replays golden frame 60 through the receive path and
// requires the master's name to come back out. The pre-fix decoder accepted only IPX type
// 0x14 and dropped this frame — the one frame in the whole exchange that names the master —
// which is why an NBIPX FindMaster reported nothing even once the request was addressed
// correctly. NBF never showed the bug: its reply rides the same UI datagram as its request.
func TestDecodeGoldenBackupListResponse(t *testing.T) {
	t.Parallel()
	frame, err := hex.DecodeString(goldenBackupListResponse)
	if err != nil {
		t.Fatalf("golden frame: %v", err)
	}
	for _, proto := range []Protocol{NBIPX, IPX} {
		t.Run(string(proto), func(t *testing.T) {
			c := &Conn{proto: proto, srcMAC: RandomMAC(), srcName: nb.NewName("WIN98-2", NameTypeWorkstation)}
			payload, addr := c.browserDatagram(frame)
			if payload == nil {
				t.Fatal("golden type-4 GetBackupList response was rejected by the datagram decoder")
			}
			if addr != "00000000.00:86:b0:ae:29:6f" {
				t.Errorf("source address = %q, want the master's IPX net.node", addr)
			}
			w, err := mailslotproto.Unmarshal(payload)
			if err != nil || !strings.EqualFold(w.Name, mailslotproto.NameBrowse) {
				t.Fatalf("mailslot = %q err=%v", w.Name, err)
			}
			op, body, ok := browserproto.UnwrapPayload(w.Body)
			if !ok || op != browserproto.OpGetBackupListResp {
				t.Fatalf("browser op = %#x ok=%t, want GetBackupListResp %#x", op, ok, browserproto.OpGetBackupListResp)
			}
			resp, err := browserproto.UnmarshalGetBackupListResponse(body)
			if err != nil {
				t.Fatalf("UnmarshalGetBackupListResponse: %v", err)
			}
			if len(resp.BackupServers) != 1 || browserproto.NormalizeName(resp.BackupServers[0]) != "WIN98-1" {
				t.Fatalf("backup servers = %v, want [WIN98-1] (the master names itself first)", resp.BackupServers)
			}
		})
	}
}

// recordLink is a FrameLink that keeps every written frame, so a test can assert on the
// whole burst a multi-datagram step emits rather than only its last frame.
type recordLink struct{ frames [][]byte }

func (l *recordLink) Write(f []byte) error {
	l.frames = append(l.frames, append([]byte(nil), f...))
	return nil
}
func (l *recordLink) Read() ([]byte, error) { return nil, nil }
func (l *recordLink) Close() error          { return nil }

// mentions reports whether any recorded frame carries name as its destination.
func (l *recordLink) mentions(name nb.Name) bool {
	for _, f := range l.frames {
		if strings.Contains(string(f), string(name[:])) {
			return true
		}
	}
	return false
}
