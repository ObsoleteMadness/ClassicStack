package netbios

import (
	"errors"
	"strings"
	"time"

	corelink "github.com/ObsoleteMadness/ClassicStack/core/link"
	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	mailslotproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// masterbrowse.go is the master-browser-driven half of "net view": rather than only
// broadcasting an AnnouncementRequest and hoping every host re-announces (Browse, in
// browser.go), it locates the segment's master browser and asks IT who is on the workgroup.
// In a real Windows/OS-2 workgroup an ordinary host announces ONLY to the local master
// browser (a directed datagram to <workgroup><1D>), never to the broadcast address, so a
// broadcast solicit sees far fewer servers than the master's list holds. The three steps
// mirror what "net view" / smbclient -L do:
//
//  1. Find the master browser — a directed AnnouncementRequest to the workgroup's
//     master-browser name <workgroup><1D> and to the special __MSBROWSE__ segment-master
//     group name; collect who claims the master / domain-master role.
//  2. __MSBROWSE__ check — the well-known group name every segment master registers; a
//     responder to it is a master browser for this segment.
//  3. GetBackupList — ask the master for its backup browsers, so a caller can then run a
//     RAP NetServerEnum2 against any of them (the authoritative server list) over an SMB
//     session (client/smb).
//
// This client is deliberately PASSIVE: it never sends a RequestElection and never claims a
// browser role, so it can never be elected master. A browse station is ephemeral — it
// observes the segment's browsers, it does not join the browser cohort.

// msBrowseName is the special __MSBROWSE__ group name every segment master browser
// registers ([MS-BRWS] §2.1.1): the 15 visible bytes are 0x01 0x02 "__MSBROWSE__" 0x02,
// and the type suffix is 0x01. It is built as raw bytes (not via nb.NewName, which would
// upper-case and space-pad and so corrupt the 0x01/0x02 framing bytes). A directed
// datagram to this name reaches every master browser on the segment.
var msBrowseName = func() nb.Name {
	var n nb.Name
	n[0] = 0x01
	n[1] = 0x02
	copy(n[2:], "__MSBROWSE__")
	n[14] = 0x02
	n[15] = 0x01 // the <01> master-browser-of-segment suffix
	return n
}()

// nameTypeLocalMaster is the NetBIOS suffix (<1D>) the local master browser of a workgroup
// registers; a directed AnnouncementRequest to <workgroup><1D> reaches it specifically.
const nameTypeLocalMaster = browserproto.NameTypeMasterBrowser

// getBackupListRequestedCount is how many backup browsers we ask the master to return; a
// small segment usually has one or two, but the master returns as many as it knows.
const getBackupListRequestedCount = 8

// getBackupListToken is an arbitrary correlation token echoed in the GetBackupList reply;
// it lets a listener match a reply to this request (any non-zero value works).
const getBackupListToken uint32 = 0x43535442 // "CSTB"

// defaultWorkgroup is the workgroup a directed browser probe targets when the caller
// pinned none and the sniff learned none. A GetBackupList / AnnouncementRequest MUST be a
// directed datagram to <workgroup><1D> — the master only answers a request bearing its
// registered local-master name, never a wildcard group broadcast (which it is not
// registered for and silently drops, captures/win98nbf-win31nbf.pcapng frame 25). WORKGROUP
// is the near-universal default on legacy Win/WfW/OS-2 segments, so it is the best blind
// target; a caller that knows its workgroup passes it and this is not used.
const defaultWorkgroup = "WORKGROUP"

// workgroupOrDefault substitutes defaultWorkgroup for an unknown workgroup, so every
// name-building path targets a real workgroup label rather than an empty one.
func workgroupOrDefault(workgroup string) string {
	if w := strings.TrimSpace(workgroup); w != "" {
		return w
	}
	return defaultWorkgroup
}

// masterTarget is the destination a master-directed browser datagram (AnnouncementRequest,
// GetBackupList) uses on this carrier, plus whether that name is a FAN-OUT address rather
// than the master's own registered name.
//
// Over NBF it is the master's <workgroup><1D>, directed: the master answers only a request
// bearing its registered local-master name (captures/win98nbf-win31nbf.pcapng frames
// 25→26). The NWLink IPX datagram plane has no <1D> form at all — golden Win98 sends its
// GetBackupList to <workgroup><00> on socket 0x0553 and the master answers it
// (spec/captures/nbipx-win98.pcap frames 58→60, nwlink-win98.pcap frames 40→41), while
// the <1D>-directed copy it sends in parallel rides a different plane entirely (socket
// 0x0455, the bare NBIPXDirectedDatagram form, frames 57→63). So on IPX the master is
// reached by fanning out to the workgroup and letting it self-select.
func (c *Conn) masterTarget(workgroup string) (nb.Name, bool) {
	if ipxFamily(c.proto) {
		return c.browseFanoutName(workgroup), true
	}
	return nb.NewName(workgroupOrDefault(workgroup), nameTypeLocalMaster), false
}

// MasterInfo is what a master-browser probe found on one carrier: which host is acting as
// the (local) master browser, the workgroup it serves, and the backup browsers it named.
// Any field may be empty when the segment answered only partially (a common case on a
// quiet segment with a single browser).
type MasterInfo struct {
	Protocol       Protocol // the carrier this was learned on
	Workgroup      string   // the domain/workgroup the master serves (from a domain announce)
	MasterName     string   // the local/segment master browser's server name
	MasterAddress  string   // its protocol source address (MAC or IPX net.node)
	BackupBrowsers []string // backup-browser names the master returned (GetBackupList)
}

// FindMaster locates the master browser reachable over this carrier and its backup
// browsers. It sends a directed AnnouncementRequest to the workgroup master-browser name
// and to __MSBROWSE__ (so the segment master re-announces immediately), plus a broadcast
// AnnouncementRequest, then listens for local-master / domain announcements to identify the
// master, and finally sends a GetBackupList to that master to collect the backup browsers.
// workgroup is the domain to target ("" solicits any). It never sends an election frame.
//
// A caller uses the returned MasterName / BackupBrowsers as the servers to run a RAP
// NetServerEnum2 against (over an SMB session) for the authoritative server list.
func (c *Conn) FindMaster(workgroup string, window time.Duration) (MasterInfo, error) {
	info := MasterInfo{Protocol: c.proto, Workgroup: workgroup}

	// Step 1 + 2: solicit the master browser. A broadcast solicit reaches every listening
	// browser; the directed solicits to <workgroup><1D> and __MSBROWSE__ specifically poke
	// the local and segment masters, which is what draws a LocalMasterAnnouncement.
	if err := c.solicitMasters(workgroup); err != nil {
		return info, err
	}

	// Listen for master/domain announcements over the first part of the window to learn who
	// the master is, before asking it for its backup list.
	half := window / 2
	if half <= 0 {
		half = window
	}
	deadline := time.Now().Add(half)
	for time.Now().Before(deadline) {
		frame, err := c.fl.Read()
		if err != nil {
			if errors.Is(err, corelink.ErrTimeout) {
				continue
			}
			break
		}
		h := c.decodeFrame(frame)
		if h == nil {
			continue
		}
		switch h.Role {
		case "master":
			if info.MasterName == "" {
				info.MasterName = h.Name
				info.MasterAddress = h.Address
			}
		case "domain master":
			// A domain announcement names the local master and (in its comment) the
			// workgroup; prefer it for the workgroup label, and adopt its master if we have
			// none yet.
			if info.Workgroup == "" && h.Comment != "" {
				info.Workgroup = h.Comment
			}
			if info.MasterName == "" {
				info.MasterName = h.Name
				info.MasterAddress = h.Address
			}
		}
	}

	// Step 3: ask the master for its backup browsers. GetBackupList is a directed datagram
	// to the master-browser name; the reply lists the backup browsers a caller can query.
	if err := c.requestBackupList(workgroup); err != nil {
		return info, err
	}
	deadline = time.Now().Add(window - half)
	for time.Now().Before(deadline) {
		frame, err := c.fl.Read()
		if err != nil {
			if errors.Is(err, corelink.ErrTimeout) {
				continue
			}
			break
		}
		if servers, ok := c.decodeBackupList(frame); ok {
			for _, s := range servers {
				name := browserproto.NormalizeName(s)
				if name != "" {
					info.BackupBrowsers = append(info.BackupBrowsers, name)
				}
			}
			// The GetBackupList response IS the master's answer, so it identifies the
			// master even when no LocalMasterAnnounce was heard — the common real-wire
			// case (captures/win98nbf-win31nbf.pcapng frame 26: WIN98-NBF answers naming
			// itself, and WIN311 mounts it without ever receiving a 0x0F announcement). A
			// master lists itself first in its own backup list ([MS-BRWS] §3.2.5.5), so
			// adopt the first named server as the master when step 1 found none.
			if info.MasterName == "" && len(info.BackupBrowsers) > 0 {
				info.MasterName = info.BackupBrowsers[0]
				if src := c.backupListSource(frame); src != "" {
					info.MasterAddress = src
				}
			}
		}
		// A master may also re-announce here; capture it if step 1 missed it.
		if h := c.decodeFrame(frame); h != nil && h.Role == "master" && info.MasterName == "" {
			info.MasterName = h.Name
			info.MasterAddress = h.Address
		}
	}
	return info, nil
}

// solicitMasters sends the AnnouncementRequests that poke the master browser: a broadcast
// one (every listening browser), a directed one to <workgroup><1D> (the local master), and
// a directed one to __MSBROWSE__ (the segment master). The ResponseName is our station's
// computer name — a real browser rejects an AnnouncementRequest with no response name and
// never re-announces, so it must be populated (see Conn.announcementRequestBody).
func (c *Conn) solicitMasters(workgroup string) error {
	body := c.announcementRequestBody()
	// Fan-out solicit (all browsers): "*"<1E> on NBF, <workgroup><00> on the IPX plane.
	if err := c.SendMailslot(mailslotproto.NameBrowse, c.browseFanoutName(workgroup), body, true); err != nil {
		return err
	}
	// Solicit aimed at the master. On NBF that is a directed datagram to the
	// <1D>-registered local master (a wildcard broadcast never reaches it — see
	// requestBackupList); on the IPX plane masterTarget returns the same workgroup
	// fan-out name, so this repeats the frame above and is skipped.
	master, fanout := c.masterTarget(workgroup)
	if !fanout {
		dtracef("%s browser AnnouncementRequest → %s (local master)", c.proto, master.String())
		if err := c.SendMailslot(mailslotproto.NameBrowse, master, body, false); err != nil {
			return err
		}
	}
	// Directed solicit to the segment master via the __MSBROWSE__ special name. It is an
	// NBF-only step here: golden NWLink puts __MSBROWSE__ on the NB-IPX SESSION socket
	// 0x0455 as a bare directed datagram (spec/captures/nbipx-nt351-win98.pcap frame 54),
	// never as an NMPI MailslotSend on 0x0553, so emitting it on this plane would put a
	// frame on the wire no real stack sends.
	if ipxFamily(c.proto) {
		return nil
	}
	dtracef("%s browser AnnouncementRequest → __MSBROWSE__ (segment master)", c.proto)
	return c.SendMailslot(mailslotproto.NameBrowse, msBrowseName, body, true)
}

// requestBackupList sends a GetBackupList datagram to the master browser, asking for its
// backup browsers. It is ALWAYS directed to the local-master-browser name <workgroup><1D>
// (defaultWorkgroup<1D> when the caller knows no workgroup) — a real master answers a
// GetBackupList only when it is addressed to its registered <1D> name, not to a wildcard
// group (captures/win98nbf-win31nbf.pcapng frames 25→26). The datagram still fans out to
// the functional multicast MAC; the destination NAME is what the master matches on.
func (c *Conn) requestBackupList(workgroup string) error {
	body := browserproto.GetBackupListRequest{
		RequestedCount: getBackupListRequestedCount,
		Token:          getBackupListToken,
	}.Marshal()
	dst, fanout := c.masterTarget(workgroup)
	dtracef("%s browser GetBackupList → %s", c.proto, dst.String())
	// NBF: broadcast=false — a directed (0x08) datagram by name; sendNBF fans it to the
	// multicast MAC regardless, so every node — including the master — receives it.
	// IPX: broadcast=true — the fan-out name is typed NMPINameTypeWorkgroup, as golden's is.
	return c.SendMailslot(mailslotproto.NameBrowse, dst, body, fanout)
}

// decodeBackupList strips this carrier's framing from one inbound frame and, if it carries
// a browser GetBackupList RESPONSE for our token, returns the backup-browser server names.
// It mirrors decodeFrame but pulls the browser payload as a GetBackupListResponse.
func (c *Conn) decodeBackupList(frame []byte) ([]string, bool) {
	payload := c.browserPayload(frame)
	if payload == nil {
		return nil, false
	}
	// payload is the SMB_COM_TRANSACTION mailslot write; unwrap the \MAILSLOT\BROWSE
	// envelope to reach the bare browser frame (the same step announcementToHost does).
	w, err := mailslotproto.Unmarshal(payload)
	if err != nil || !strings.EqualFold(w.Name, mailslotproto.NameBrowse) {
		return nil, false
	}
	op, body, ok := browserproto.UnwrapPayload(w.Body)
	if !ok || op != browserproto.OpGetBackupListResp {
		return nil, false
	}
	resp, err := browserproto.UnmarshalGetBackupListResponse(body)
	if err != nil || resp.Token != getBackupListToken {
		return nil, false
	}
	return resp.BackupServers, true
}

// backupListSource returns the printable source address (MAC for NBF, IPX net.node for
// NBIPX) the frame arrived from — the master browser's L2/L3 address, recorded on the
// MasterInfo so a caller can render "master via <addr>". Empty when the frame is not a
// datagram for this carrier.
func (c *Conn) backupListSource(frame []byte) string {
	_, addr := c.browserDatagram(frame)
	return addr
}
