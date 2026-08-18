// Package browse is the client SDK's "net view": it enumerates the SMB servers on a legacy
// segment the way Windows actually does — through the MASTER BROWSER, not by trusting
// broadcast self-announcements. In a real workgroup an ordinary host (e.g. a Win98 File &
// Print station) announces ONLY to the local master browser, on a ~12-minute periodic timer,
// and does NOT answer a broadcast AnnouncementRequest. So a solicit-and-sniff sweep almost
// never sees it; the authoritative list lives in the master browser and must be asked for.
//
// Over each NetBIOS datagram carrier (NBF over 802.2 LLC, NBIPX over IPX) Enumerate runs
// three sources and merges them:
//
//  1. solicit + sniff browser announcements (client/netbios.Conn.Browse) — catches any host
//     that announces to the segment during the window and identifies the masters;
//  2. find the master browser (client/netbios.Conn.FindMaster: directed AnnouncementRequest
//     to <workgroup><1D> and __MSBROWSE__, then GetBackupList);
//  3. ask that master (or a backup browser) for the authoritative server list — RAP
//     NetServerEnum2 over an SMB IPC$ session to it (client/smb.EnumServers). THIS is what
//     surfaces the quiet ordinary hosts a solicit misses.
//
// It is deliberately PASSIVE — it never sends a browser election, so it is never electable
// as master (a browse client is ephemeral). Both cmd/csnetview and cmd/csclient's
// `discover smb` are thin consumers of Enumerate: the wire orchestration lives here, once.
//
// Ring: CLIENT — it imports both client/netbios (the datagram carrier) and client/smb (the
// session carrier for NetServerEnum2), which is why it sits above them rather than inside
// either.
package browse

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
	browserproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/browser"
	nb "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// Options configures an Enumerate sweep. Device/Kind/MAC/FrameType describe the raw NIC to
// browse over (the same fields the file client's opener takes); Window is how long to listen
// per carrier after soliciting; Workgroup is the domain to target ("" solicits any master
// and enumerates the master's own primary domain). Trace, when non-nil, receives one-line
// progress messages ("[nbf] locating master browser ...") so a CLI can echo the steps.
type Options struct {
	Device    string
	Kind      string
	MAC       [6]byte
	FrameType string
	Window    time.Duration
	Workgroup string
	Trace     func(string)
	// Carriers restricts the NetBIOS datagram sweep to these protocols (nbf, nbipx).
	// Empty runs every datagram carrier in netbios.Protocols.
	Carriers []netbios.Protocol
}

// Protocol is the NetBIOS datagram carrier a server was heard/queried on (re-exported from
// client/netbios so a consumer renders the carrier without importing that package too).
type Protocol = netbios.Protocol

// Source records how a server was discovered, most-authoritative first: from a master's
// browse list (NetServerEnum2), as a master browser itself, or from a broadcast announcement
// caught during the sniff.
type Source int

const (
	SourceAnnouncement Source = iota // heard a broadcast Host/Domain announcement
	SourceMaster                     // identified as a/the master browser
	SourceBrowseList                 // returned by a master's RAP NetServerEnum2 (authoritative)
)

// Server is one discovered SMB server, aggregated across carriers and sources.
type Server struct {
	Name      string             // upper-cased NetBIOS server name
	Carriers  []netbios.Protocol // the carriers it was heard/queried on
	Source    Source             // the most authoritative source that saw it
	Comment   string             // operator comment, if any source carried one
	Role      string             // "master browser" / "backup browser" / "" (ordinary)
	OSVersion string             // "major.minor" from an announcement, if seen
	Address   string             // TCP/IP: IPv4 of the host when known (NBNS); empty on NBF/NBIPX
	addrs     map[netbios.Protocol]string
}

// AddressFor is the protocol source address for one carrier: MAC on NBF, IPX net.node
// on NBIPX, IPv4 on TCP. Empty when that carrier was only learned from a browse list.
func (s Server) AddressFor(p netbios.Protocol) string {
	if s.addrs != nil {
		if a := strings.TrimSpace(s.addrs[p]); a != "" {
			return a
		}
	}
	if p == netbios.TCP {
		return strings.TrimSpace(s.Address)
	}
	return ""
}

// Result is the outcome of one carrier's sweep: the master browser it found (if any), its
// backup browsers, and any per-source error (for the CLI to surface). Servers are returned
// separately from Enumerate as the merged union.
type Result struct {
	Protocol       netbios.Protocol
	MasterName     string
	BackupBrowsers []string
	Err            error // a carrier-open failure; the sweep of other carriers still ran
}

// Enumerate performs the full net-view sweep over every NetBIOS carrier and returns the
// merged, name-sorted server union plus a per-carrier Result (master browser, backups,
// errors). It never returns a fatal error — a carrier that cannot open is reported in its
// Result.Err, and the other carrier still runs.
func Enumerate(opts Options) ([]Server, []Result) {
	window := opts.Window
	if window <= 0 {
		window = 4 * time.Second
	}
	carriers := opts.Carriers
	if len(carriers) == 0 {
		carriers = netbios.Protocols
	}
	opener, err := netbios.OpenerFor(opts.Kind, opts.Device, opts.MAC)
	if err != nil {
		// Every carrier fails identically when the opener cannot be built.
		results := make([]Result, 0, len(carriers))
		for _, p := range carriers {
			results = append(results, Result{Protocol: p, Err: err})
		}
		return nil, results
	}
	station := netbios.DefaultStationName(opener.MAC, netbios.NameTypeWorkstation)

	agg := map[string]*Server{}
	results := make([]Result, 0, len(carriers))
	for _, p := range carriers {
		results = append(results, enumerateCarrier(opener, station, p, opts, window, agg))
	}
	return sortedServers(agg), results
}

// enumerateCarrier runs the three sources over one carrier, merging finds into agg.
func enumerateCarrier(opener *clientlink.Opener, station nb.Name, p netbios.Protocol, opts Options, window time.Duration, agg map[string]*Server) Result {
	res := Result{Protocol: p}
	tracef(opts, "[%s] soliciting browser announcements (%s) ...", p, window)
	c, err := netbios.Open(opener, p, station)
	if err != nil {
		res.Err = err
		return res
	}

	// Source 1: solicit + sniff announcements (self-announcers + masters).
	hosts, _ := c.Browse(window)
	for _, h := range hosts {
		merge(agg, h.Name, p, SourceAnnouncement, hostRole(h), h.Comment, h.OSVersion, h.Address)
	}

	// Learn the workgroup to target if the caller pinned none: a real GetBackupList /
	// AnnouncementRequest is a DIRECTED datagram to <workgroup><1D> (the local master's
	// registered name), not a broadcast to a wildcard group — the master only answers a
	// request bearing its own name (captures/win98nbf-win31nbf.pcapng frames 19/25). The
	// sniff's Host/Domain announcements carry the workgroup, so adopt it before FindMaster.
	workgroup := opts.Workgroup
	if workgroup == "" {
		workgroup = workgroupFromHosts(hosts)
	}

	// Source 2: find the master browser (<workgroup><1D> directed + __MSBROWSE__ + GetBackupList).
	tracef(opts, "[%s] locating master browser ...", p)
	master, _ := c.FindMaster(workgroup, window)
	_ = c.Close() // done with the datagram carrier; the SMB session opens its own FrameLink
	res.MasterName = master.MasterName
	res.BackupBrowsers = master.BackupBrowsers
	if master.MasterName != "" {
		tracef(opts, "[%s] master browser: %s%s", p, master.MasterName, backupNote(master))
		merge(agg, master.MasterName, p, SourceMaster, "master browser", "", "", master.MasterAddress)
	}

	// Source 3: ask the master (or a backup browser) for the authoritative server list.
	for _, tgt := range enumTargets(master) {
		tracef(opts, "[%s] asking %s for the server list (NetServerEnum2 over IPC$) ...", p, tgt)
		servers, err := enumServers(opts, p, tgt, master.Workgroup)
		if err != nil {
			tracef(opts, "[%s] %s: NetServerEnum2 failed: %v", p, tgt, err)
			continue
		}
		if len(servers) == 0 {
			tracef(opts, "[%s] %s returned an empty server list", p, tgt)
			continue
		}
		tracef(opts, "[%s] %s returned %d servers (NetServerEnum2)", p, tgt, len(servers))
		for _, s := range servers {
			merge(agg, s.Name, p, SourceBrowseList, serverRole(s), s.Comment, "", "")
		}
		break // one authoritative list per carrier is enough
	}
	return res
}

// enumTargets is the ordered list of browsers to try a NetServerEnum2 against: the master
// first, then any backup browsers it named (a backup holds the same list, so it is the
// fallback when the master itself does not accept an SMB session).
func enumTargets(m netbios.MasterInfo) []string {
	targets := make([]string, 0, 1+len(m.BackupBrowsers))
	if m.MasterName != "" {
		targets = append(targets, m.MasterName)
	}
	for _, b := range m.BackupBrowsers {
		if b != m.MasterName {
			targets = append(targets, b)
		}
	}
	return targets
}

// enumServers opens an anonymous SMB IPC$ session to master over carrier p and runs RAP
// NetServerEnum2, returning the browse-list servers. A browse needs no credentials — the
// master answers an anonymous query with its full list. The error is returned (not
// swallowed) so the caller can trace WHY a master that was found did not yield a list.
func enumServers(opts Options, p netbios.Protocol, master, workgroup string) ([]clientsmb.BrowseServer, error) {
	spec := clientlink.Spec{Kind: opts.Kind, Name: opts.Device, Carrier: string(p), FrameType: opts.FrameType}
	opener := clientlink.NewOpener(spec)
	if opts.MAC != ([6]byte{}) {
		opener.MAC = opts.MAC
	}
	return clientsmb.EnumServers(opener, master, workgroup, "", "")
}

// merge inserts or enriches a discovered server, keeping the most authoritative source and
// not losing a richer field (comment/role/version) to a sparser later one.
func merge(agg map[string]*Server, name string, p netbios.Protocol, src Source, role, comment, osVersion, addr string) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return
	}
	s := agg[name]
	if s == nil {
		s = &Server{Name: name}
		agg[name] = s
	}
	s.Carriers = addCarrier(s.Carriers, p)
	if src > s.Source {
		s.Source = src
	}
	if comment != "" {
		s.Comment = comment
	}
	if role != "" && role != "host" {
		s.Role = role
	}
	if osVersion != "" && osVersion != "0.0" {
		s.OSVersion = osVersion
	}
	addr = strings.TrimSpace(addr)
	if addr != "" {
		if s.addrs == nil {
			s.addrs = map[netbios.Protocol]string{}
		}
		s.addrs[p] = addr
		if p == netbios.TCP {
			s.Address = addr
		}
	}
}

// addCarrier appends p if not already present (a server heard on two carriers lists both).
func addCarrier(cs []netbios.Protocol, p netbios.Protocol) []netbios.Protocol {
	if slices.Contains(cs, p) {
		return cs
	}
	return append(cs, p)
}

// workgroupFromHosts extracts the workgroup name from a sniffed host list so a subsequent
// GetBackupList can be directed to <workgroup><1D>. A DomainAnnouncement's Host carries it
// as a "workgroup X" comment (announcementToHost stamps that form); a plain host/master
// announcement does not name its workgroup on the wire, so the domain announce is the one
// reliable source. Returns "" when no host named a workgroup (the caller then broadcasts).
func workgroupFromHosts(hosts []netbios.Host) string {
	const prefix = "workgroup "
	for _, h := range hosts {
		if strings.HasPrefix(strings.ToLower(h.Comment), prefix) {
			if wg := strings.TrimSpace(h.Comment[len(prefix):]); wg != "" {
				return wg
			}
		}
	}
	return ""
}

// hostRole maps an announcement's role to a server Role label ("" for an ordinary host).
func hostRole(h netbios.Host) string {
	switch h.Role {
	case "master", "domain master":
		return "master browser"
	default:
		return ""
	}
}

// serverRole maps a NetServerEnum2 SV_TYPE-word to a Role label.
func serverRole(s clientsmb.BrowseServer) string {
	switch {
	case s.Type&browserproto.ServerTypeMasterBrowser != 0:
		return "master browser"
	case s.Type&browserproto.ServerTypeBackupBrowser != 0:
		return "backup browser"
	default:
		return ""
	}
}

// backupNote renders a master's backup-browser list as a " (backups: ...)" suffix.
func backupNote(m netbios.MasterInfo) string {
	if len(m.BackupBrowsers) == 0 {
		return ""
	}
	return " (backups: " + strings.Join(m.BackupBrowsers, ", ") + ")"
}

// sortedServers flattens the aggregation map into a name-sorted slice.
func sortedServers(agg map[string]*Server) []Server {
	out := make([]Server, 0, len(agg))
	for _, s := range agg {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// tracef emits one progress line through opts.Trace when set.
func tracef(opts Options, format string, args ...any) {
	if opts.Trace == nil {
		return
	}
	opts.Trace(fmt.Sprintf(format, args...))
}
