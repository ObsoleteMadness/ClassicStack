// Package smb is the SMB 1.0 file-server stub. It is not an AppleTalk
// service and does not consume DDP datagrams; it rides NetBIOS (today
// NBT only — see service/netbios/over_tcp) and exposes file shares
// backed by the shared pkg/vfs registry.
//
// The package is a stub: NewService produces a Service whose Start
// runs a no-op lifecycle, dispatch returns STATUS_NOT_SUPPORTED for
// every SMB command, and the authenticator is a permissive guest stub.
package smb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// ErrNotImplemented is returned by stub call sites that have not
// been filled in.
var ErrNotImplemented = errors.New("smb: not implemented")

// originSMB is the publisher tag used on every vfs.Event the SMB
// server emits, so subscribers (including this one) can filter their
// own events out and avoid feedback loops.
const originSMB = "smb"

const hostAnnouncementPeriod = 2 * time.Minute

type browserRole uint8

const (
	browserRolePotential browserRole = iota
	browserRoleBackup
	browserRoleLocalMaster
)

const (
	browserNameTypeMasterBrowser = 0x1D

	browserCommandHostAnnouncement    = 0x01
	browserCommandAnnouncementReq     = 0x02
	browserCommandRequestElection     = 0x08
	browserCommandGetBackupListReq    = 0x09
	browserCommandGetBackupListResp   = 0x0A
	browserCommandDomainAnnouncement  = 0x0C
	browserCommandLocalMasterAnnounce = 0x0F
	browserVersionElection            = 0x01
	browserVersionMajor               = 0x0F
	browserVersionMinor               = 0x01
	hostAnnouncementVersionMajor      = 0x15
	hostAnnouncementVersionMinor      = 0x04
	browserSignature                  = 0xAA55
	browserServerTypeWorkstationMask  = 0x00402003
	browserElectionCriteriaMasterMask = 0x00000004
	browserServerTypeBackupMask       = 0x00020000
	browserServerTypeMasterMask       = 0x00040000
	browserServerTypeDomainEnumMask   = 0x80000000

	browserMailslotBrowse = "\\MAILSLOT\\BROWSE"
	browserMailslotLANMAN = "\\MAILSLOT\\LANMAN"

	smbHeaderLen                 = 32
	browserTransactionWordCount  = 17
	browserTransactionWordsLen   = 34
	browserTransactionByteOffset = smbHeaderLen + 1 + browserTransactionWordsLen
	browserTransactionDataOffset = 86

	smbStatusSuccess        = 0x00000000
	smbStatusBadTID         = 0x00050002
	smbStatusNotSupported   = 0xC00000BB
	smbStatusBadNetworkName = 0xC00000CC // STATUS_BAD_NETWORK_NAME
	smbStatusNoMoreFiles    = 0x80000006 // STATUS_NO_MORE_FILES
	smbStatusErrBadFunc     = 0x00010001 // ERRDOS/ERRbadfunc
	smbStatusErrBadFile     = 0x00020001 // ERRDOS/ERRbadfile
	smbStatusErrBadPath     = 0x00030001 // ERRDOS/ERRbadpath
	smbStatusErrNoAccess    = 0x00050001 // ERRDOS/ERRnoaccess
	smbStatusErrNoFiles     = 0x00120001 // ERRDOS/ERRnofiles
	smbStatusErrInvNetName  = 0x00430001 // ERRDOS/ERRinvnetname
	smbStatusErrSrvError    = 0x00010002 // ERRSRV/ERRerror
	smbStatusUseStandard    = 0x00FB0002 // ERRSRV/ERRuseSTD — fall back to SMB_COM_READ/WRITE
	smbStatusInvalidHandle  = 0xC0000008 // STATUS_INVALID_HANDLE
	smbStatusErrBadFid      = 0x00060001 // ERRDOS/ERRbadfid — invalid FID

	// SMB1 NEGOTIATE capability bits ([MS-CIFS] 2.2.4.52.2). All defined
	// flags are listed for documentation; only a curated subset is
	// actually OR'd into the advertised Capabilities field below.
	capRawMode              = uint32(0x00000001) // CAP_RAW_MODE — server supports SMB_COM_READ_RAW / WRITE_RAW
	capMpxMode              = uint32(0x00000002) // CAP_MPX_MODE — server supports SMB_COM_READ_MPX / WRITE_MPX
	capUnicode              = uint32(0x00000004) // CAP_UNICODE — server supports Unicode strings
	capLargeFiles           = uint32(0x00000008) // CAP_LARGE_FILES — server supports 64-bit file offsets
	capNTSMBs               = uint32(0x00000010) // CAP_NT_SMBS — server supports the NT-mode SMBs
	capRPCRemoteAPI         = uint32(0x00000020) // CAP_RPC_REMOTE_APIS
	capStatus32             = uint32(0x00000040) // CAP_STATUS32 — server returns 32-bit NTSTATUS
	capLevel2Oplocks        = uint32(0x00000080) // CAP_LEVEL_II_OPLOCKS
	capLockAndRead          = uint32(0x00000100) // CAP_LOCK_AND_READ
	capNTFind               = uint32(0x00000200) // CAP_NT_FIND
	capDFS                  = uint32(0x00001000) // CAP_DFS
	capInfoLevelPassthrough = uint32(0x00002000) // CAP_INFOLEVEL_PASSTHRU
	capLargeReadX           = uint32(0x00004000) // CAP_LARGE_READX
	capLargeWriteX          = uint32(0x00008000) // CAP_LARGE_WRITEX
	capLwio                 = uint32(0x00010000) // CAP_LWIO
	capUnix                 = uint32(0x00800000) // CAP_UNIX
	capDynamicReauth        = uint32(0x20000000) // CAP_DYNAMIC_REAUTH
	capExtendedSecurity     = uint32(0x80000000) // CAP_EXTENDED_SECURITY

	// negotiateCapabilities is the exact set we advertise. We deliberately
	// do NOT advertise CAP_RAW_MODE or CAP_MPX_MODE — both legacy
	// transports (read/write raw, read/write mpx) are unimplemented and
	// silently corrupt files when half-emulated. Win9x falls back to
	// SMB_COM_READ / SMB_COM_WRITE / SMB_COM_WRITE_ANDX when those bits
	// are clear.
	negotiateCapabilities = capNTSMBs |
		capStatus32 |
		capNTFind |
		capLargeFiles

	// SMB1 NEGOTIATE numeric parameters. These match SMBLibrary defaults
	// and are conservative enough to keep Win9x clients happy on a
	// connectionless transport (Direct IPX) where larger windows just
	// invite retransmission storms.
	negotiateMaxMpxCount   = uint16(1)      // single-request server, no parallel commands
	negotiateMaxNumberVcs  = uint16(1)      // one virtual circuit per session
	negotiateMaxBufferSize = uint32(0x4000) // 16 KiB per request
	negotiateMaxRawSize    = uint32(0)      // raw mode disabled (paired with no CAP_RAW_MODE)

	// SecurityMode bits ([MS-CIFS] 2.2.4.52.2). User-level security with
	// no challenge: clients send credentials in the clear which we accept
	// as a guest session.
	negotiateSecurityMode = byte(0x01) // bit 0: SECURITY_MODE_USER_SECURITY

	// windowsFiletimeOffset is the difference in 100-nanosecond intervals
	// between the Windows FILETIME epoch (1 Jan 1601) and the Unix epoch
	// (1 Jan 1970).
	windowsFiletimeOffset = uint64(116444736000000000)

	// ipcShareName is the virtual IPC$ share that is always available.
	ipcShareName = "IPC$"
	// ipcShareIdx is the sentinel shareIdx stored in treeSlot for IPC$ connections.
	// It is never a valid index into the shares slice.
	ipcShareIdx = -1

	// RAP-level (16-bit) error codes returned in the param Status field.
	rapStatusErrInvalidFunction = uint16(1)  // ERROR_INVALID_FUNCTION
	rapStatusErrReqNotAccepted  = uint16(71) // ERROR_REQ_NOT_ACCEP

	// SMB1 header field byte offsets (within the 32-byte SMB1 header).
	// On a connectionless transport the SecurityFeatures region holds
	// Key(4) + CID(2) + SequenceNumber(2) at offsets 14..21 per
	// [MS-CIFS] 2.2.3.1; SequenceNumber identifies the final request of
	// a multiplexed write sequence (see SMB_COM_WRITE_MPX).
	smbOffStatus         = 5
	smbOffFlags          = 9
	smbOffFlags2         = 10
	smbOffSequenceNumber = 20
	smbOffTID            = 24
	smbOffUID            = 28

	smbFlags2KnowsLongNames = 0x0001
	smbFlags2NTStatus       = 0x4000

	// rapNetShareEnum is the RAP function code for NetShareEnum.
	rapNetShareEnum = uint16(0x0000)
	// rapNetServerEnum2 is the RAP function code for NetServerEnum2.
	rapNetServerEnum2 = uint16(0x0068)

	// dialectNTLM is the NT LM 0.12 dialect string.
	dialectNTLM = "NT LM 0.12"
)

type ServerOptions struct {
	// NBTBinding is the NetBIOS-over-TCP listen address (typically :139).
	NBTBinding string
	// DirectBinding is the SMB-direct (port 445) listen address. Empty
	// disables direct SMB; SMB 1.0 is conventionally NBT-only.
	DirectBinding string
	// GuestOk controls whether unauthenticated sessions are accepted.
	GuestOk bool
	// Workgroup is the announced workgroup/domain name.
	Workgroup string
	// ServerName is the announced NetBIOS server name. Falls back to
	// the NetBIOS service's own name when empty.
	ServerName string
	// Bus, when non-nil, is the VFS event bus the server publishes
	// to and subscribes from. The default is vfs.DefaultBus.
	Bus vfs.Bus
	// Shortname is the optional 8.3 mapper used when responding to
	// legacy DOS/Windows clients. Nil disables shortname mapping.
	Shortname vfs.ShortnameMapper
}

// Authenticator validates SMB credentials. The stub permits everyone.
type Authenticator interface {
	Authenticate(user, pass string) error
}

type guestAuth struct{}

func (guestAuth) Authenticate(_, _ string) error { return nil }

// Service is the SMB 1.0 server stub.
type Service struct {
	opts   ServerOptions
	nb     netbios.NameService
	nbData datagramSender
	shares []ShareConfig
	auth   Authenticator
	bus    vfs.Bus

	mu             sync.Mutex
	started        bool
	cancelEvent    func()
	announceCancel context.CancelFunc
	announceDone   chan struct{}
	nextUID        uint16
	browserRole    browserRole
	browserStarted time.Time
	electionCancel context.CancelFunc
	electionGen    uint64
	electionDelay  func(browserRole) time.Duration

	browserServers map[string]browserServerRecord
	machineGroups  map[string]machineGroupRecord

	connsMu          sync.Mutex
	conns            map[connKey]*connState
	shareFSes        map[int]vfs.FileSystem
	shareNameToIndex map[string]int
}

type machineGroupRecord struct {
	MasterBrowser string
	LastSeen      time.Time
}

type browserServerRecord struct {
	ServerType uint32
	LastSeen   time.Time
}

type netServerInfo1 struct {
	Name    string
	Type    uint32
	Comment string
}

type datagramSender interface {
	SendDatagram(d *netbiosproto.Datagram) error
	SendDirectedDatagram(d *netbiosproto.Datagram, remote netbios.DatagramEndpoint) error
}

// NewService creates a stubbed SMB service. nb may be nil when SMB is
// configured without NetBIOS (e.g. integration tests that drive the
// dispatch path directly). shares may be empty.
func NewService(opts ServerOptions, nb netbios.NameService, shares []ShareConfig) *Service {
	if opts.Bus == nil {
		opts.Bus = vfs.DefaultBus
	}
	return &Service{
		opts:           opts,
		nb:             nb,
		shares:         shares,
		auth:           guestAuth{},
		bus:            opts.Bus,
		nextUID:        1,
		browserRole:    browserRolePotential,
		browserStarted: time.Now(),
		electionDelay: func(role browserRole) time.Duration {
			switch role {
			case browserRoleLocalMaster:
				return 200 * time.Millisecond
			case browserRoleBackup:
				return 400 * time.Millisecond
			default:
				return 800 * time.Millisecond
			}
		},
		browserServers:   map[string]browserServerRecord{},
		machineGroups:    map[string]machineGroupRecord{},
		conns:            map[connKey]*connState{},
		shareFSes:        map[int]vfs.FileSystem{},
		shareNameToIndex: map[string]int{},
	}
}

// SetDatagramSender installs the NetBIOS datagram sender used for
// best-effort browser host announcements.
func (s *Service) SetDatagramSender(sender datagramSender) {
	s.mu.Lock()
	s.nbData = sender
	s.mu.Unlock()
}

// SetAuthenticator overrides the default guest authenticator.
func (s *Service) SetAuthenticator(a Authenticator) {
	if a == nil {
		a = guestAuth{}
	}
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
}

// Shares returns the share configs the service was constructed with.
func (s *Service) Shares() []ShareConfig {
	out := make([]ShareConfig, len(s.shares))
	copy(out, s.shares)
	return out
}

// Start brings the SMB service up. It registers a VFS bus subscriber
// so cross-protocol mutations (e.g. an AFP rename inside a shared
// volume) can invalidate SMB-side caches.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	if err := s.initShareBackendsLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.cancelEvent = s.bus.Subscribe(&shareEventSubscriber{shares: s.shares})
	s.started = true
	s.browserStarted = time.Now()
	sender := s.nbData
	server := s.opts.ServerName
	if server == "" {
		server = "CLASSICSTACK"
	}
	workgroup := s.opts.Workgroup
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	if sender != nil {
		announceCtx, cancel := context.WithCancel(ctx)
		s.announceCancel = cancel
		s.announceDone = make(chan struct{})
		go s.announceLoop(announceCtx, sender, server, workgroup, s.announceDone)
	}
	s.mu.Unlock()

	if s.nbData != nil {
		if err := s.sendHostAnnouncement(sender, server, workgroup); err != nil {
			netlog.Warn("[SMB] host announcement send failed: %v", err)
		}
	}
	return nil
}

func (s *Service) announceLoop(ctx context.Context, sender datagramSender, server, workgroup string, done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(hostAnnouncementPeriod):
			if err := s.sendHostAnnouncement(sender, server, workgroup); err != nil {
				netlog.Warn("[SMB] periodic host announcement send failed: %v", err)
			}
		}
	}
}

func (s *Service) sendHostAnnouncement(sender datagramSender, server, workgroup string) error {
	browser := hostAnnouncementFrame{
		UpdateCount:         0x03,
		PeriodicityMS:       uint32(hostAnnouncementPeriod / time.Millisecond),
		ServerName:          server,
		OSVersionMajor:      0x04,
		OSVersionMinor:      0x00,
		ServerType:          browserServerTypeWorkstationMask,
		BrowserVersionMajor: hostAnnouncementVersionMajor,
		BrowserVersionMinor: hostAnnouncementVersionMinor,
		Signature:           browserSignature,
	}.MarshalBinary()
	payload := browserMailslotTransaction{
		MailslotName:   browserMailslotBrowse,
		BrowserPayload: browser,
		TimeoutMS:      1000,
		Priority:       0,
		Class:          2,
	}.MarshalBinary()
	err := sender.SendDatagram(&netbiosproto.Datagram{
		Destination: netbiosproto.NewName(workgroup, browserNameTypeMasterBrowser),
		Source:      netbiosproto.NewName(server, netbiosproto.NameTypeFileServer),
		Payload:     payload,
	})
	if err == nil {
		netlog.Info("[SMB][Browser] announced host %q to workgroup %q", server, workgroup)
		s.noteBrowserServer(server, browserServerTypeWorkstationMask)
	}
	return err
}

func (s *Service) sendLocalMasterAnnouncement(sender datagramSender, server, workgroup string) error {
	browser := localMasterAnnouncementFrame{
		UpdateCount:               0x00,
		PeriodicityMS:             uint32(hostAnnouncementPeriod / time.Millisecond),
		ServerName:                server,
		OSVersionMajor:            0x04,
		OSVersionMinor:            0x00,
		ServerType:                browserServerTypeWorkstationMask | browserServerTypeMasterMask,
		BrowserConfigVersionMajor: browserVersionMajor,
		BrowserConfigVersionMinor: browserVersionMinor,
		Signature:                 browserSignature,
	}.MarshalBinary()
	payload := browserMailslotTransaction{
		MailslotName:   browserMailslotBrowse,
		BrowserPayload: browser,
		TimeoutMS:      1000,
		Priority:       0,
		Class:          2,
	}.MarshalBinary()
	err := sender.SendDatagram(&netbiosproto.Datagram{
		Destination: netbiosproto.NewName(workgroup, netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName(server, netbiosproto.NameTypeFileServer),
		Payload:     payload,
	})
	if err == nil {
		netlog.Info("[SMB][Browser] announced local master %q to workgroup %q", server, workgroup)
		s.noteBrowserServer(server, browserServerTypeWorkstationMask|browserServerTypeMasterMask)
	}
	return err
}

func (s *Service) noteBrowserServer(server string, serverType uint32) {
	name := normalizeBrowserName(server)
	if name == "" {
		return
	}
	s.mu.Lock()
	s.browserServers[name] = browserServerRecord{ServerType: serverType, LastSeen: time.Now()}
	s.mu.Unlock()
}

func (s *Service) noteMachineGroup(machineGroup, masterBrowser string) {
	group := normalizeBrowserName(machineGroup)
	if group == "" {
		return
	}
	master := normalizeBrowserName(masterBrowser)
	s.mu.Lock()
	s.machineGroups[group] = machineGroupRecord{MasterBrowser: master, LastSeen: time.Now()}
	s.mu.Unlock()
}

func (s *Service) backupServerList(self string) []string {
	selfName := normalizeBrowserName(self)
	out := []string{selfName}
	s.mu.Lock()
	for name, rec := range s.browserServers {
		if name == selfName {
			continue
		}
		if rec.ServerType&browserServerTypeBackupMask != 0 {
			out = append(out, name)
		}
	}
	s.mu.Unlock()
	return out
}

// Stop tears the service down.
func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.started {
		electionCancel := s.electionCancel
		s.electionCancel = nil
		s.mu.Unlock()
		if electionCancel != nil {
			electionCancel()
		}
		return nil
	}
	if s.cancelEvent != nil {
		s.cancelEvent()
		s.cancelEvent = nil
	}
	s.dropAllConnectionsLocked()
	announceCancel := s.announceCancel
	announceDone := s.announceDone
	electionCancel := s.electionCancel
	s.announceCancel = nil
	s.announceDone = nil
	s.electionCancel = nil
	s.started = false
	s.mu.Unlock()
	if announceCancel != nil {
		announceCancel()
	}
	if electionCancel != nil {
		electionCancel()
	}
	if announceDone != nil {
		<-announceDone
	}
	return nil
}

func (s *Service) localElectionUptime() uint32 {
	s.mu.Lock()
	started := s.browserStarted
	s.mu.Unlock()
	if started.IsZero() {
		return 1
	}
	secs := uint32(time.Since(started) / time.Second)
	if secs == 0 {
		return 1
	}
	return secs
}

// isSelfSourcedDatagram reports whether the inbound browser datagram
// was sent by this service. Browser frames are addressed to group names
// the local NetBIOS stack also listens on, so every broadcast we emit
// is re-delivered to handleDatagram. Without this guard the handler
// would react to its own transmissions and storm the network.
func (s *Service) isSelfSourcedDatagram(d *netbiosproto.Datagram) bool {
	if d == nil {
		return false
	}
	s.mu.Lock()
	server := s.opts.ServerName
	workgroup := s.opts.Workgroup
	s.mu.Unlock()
	if server == "" {
		server = "CLASSICSTACK"
	}
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	src := strings.ToUpper(strings.TrimSpace(d.Source.String()))
	if src == "" {
		return false
	}
	return src == strings.ToUpper(server) || src == strings.ToUpper(workgroup)
}

func (s *Service) localElectionFrame(server string) requestElectionFrame {
	return requestElectionFrame{
		Version:    browserVersionElection,
		Criteria:   browserElectionCriteriaMasterMask,
		Uptime:     s.localElectionUptime(),
		Reserved:   0,
		ServerName: server,
	}
}

func compareElection(local, remote requestElectionFrame) int {
	if local.Criteria > remote.Criteria {
		return 1
	}
	if local.Criteria < remote.Criteria {
		return -1
	}
	if local.Uptime > remote.Uptime {
		return 1
	}
	if local.Uptime < remote.Uptime {
		return -1
	}
	localName := strings.ToUpper(strings.TrimSpace(local.ServerName))
	remoteName := strings.ToUpper(strings.TrimSpace(remote.ServerName))
	cmp := strings.Compare(localName, remoteName)
	if cmp < 0 {
		return 1
	}
	if cmp > 0 {
		return -1
	}
	return 0
}

func (s *Service) sendElectionFrame(sender datagramSender, server, workgroup string, frame requestElectionFrame) error {
	payload := browserMailslotTransaction{
		MailslotName:   browserMailslotBrowse,
		BrowserPayload: frame.MarshalBinary(),
		TimeoutMS:      1000,
		Priority:       0,
		Class:          2,
	}.MarshalBinary()
	return sender.SendDatagram(&netbiosproto.Datagram{
		Destination: netbiosproto.NewName(workgroup, netbiosproto.NameTypeGroup),
		Source:      netbiosproto.NewName(server, netbiosproto.NameTypeFileServer),
		Payload:     payload,
	})
}

func (s *Service) startElectionLoop(sender datagramSender, server, workgroup string, originRole browserRole) {
	s.mu.Lock()
	if s.electionCancel != nil {
		s.mu.Unlock()
		return
	}
	delay := s.electionDelay(originRole)
	ctx, cancel := context.WithCancel(context.Background())
	s.electionCancel = cancel
	s.electionGen++
	gen := s.electionGen
	s.mu.Unlock()

	go s.runElectionLoop(ctx, sender, server, workgroup, gen, delay)
}

func (s *Service) stopElectionLoop() {
	s.mu.Lock()
	cancel := s.electionCancel
	s.electionCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) runElectionLoop(ctx context.Context, sender datagramSender, server, workgroup string, gen uint64, delay time.Duration) {
	for i := 0; i < 3; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if err := s.sendElectionFrame(sender, server, workgroup, s.localElectionFrame(server)); err != nil {
			netlog.Warn("[SMB][Browser] election resend failed: %v", err)
			continue
		}
	}

	s.mu.Lock()
	if s.electionGen != gen {
		s.mu.Unlock()
		return
	}
	s.electionCancel = nil
	s.browserRole = browserRoleLocalMaster
	s.mu.Unlock()

	netlog.Info("[SMB][Browser] election won after 4 request-election transmissions")
	if err := s.sendLocalMasterAnnouncement(sender, server, workgroup); err != nil {
		netlog.Warn("[SMB][Browser] local master announcement send failed: %v", err)
	}
}

// HandleSession implements netbios.CommandHandler. The stub rejects
// every inbound session-layer SMB request as not implemented.

func (s *Service) HandleDatagram(d *netbiosproto.Datagram) error {
	return s.handleDatagram(d, netbios.DatagramContext{})
}

// HandleDatagramContext implements netbios.ContextualDatagramHandler.
func (s *Service) HandleDatagramContext(d *netbiosproto.Datagram, ctx netbios.DatagramContext) error {
	return s.handleDatagram(d, ctx)
}

func (s *Service) handleDatagram(d *netbiosproto.Datagram, ctx netbios.DatagramContext) error {
	if d == nil || len(d.Payload) == 0 {
		return nil
	}
	// Drop datagrams whose source name matches our own server identity.
	// Browser frames are sent to group names (WORKGROUP<1E>, <1D>) that the
	// local stack is also subscribed to, so each broadcast is delivered
	// back to us. Without this guard, every election/announcement we emit
	// re-enters the handler, satisfies cmp >= 0, and triggers another
	// transmission — producing the storm seen in captures/netbeui.pcap
	// and captures/ipx.pcap.
	if s.isSelfSourcedDatagram(d) {
		return nil
	}
	tx, err := unmarshalBrowserMailslotTransaction(d.Payload)
	if err != nil || len(tx.BrowserPayload) == 0 {
		return nil
	}
	cmd, framePayload, ok := unwrapBrowserPayload(tx.BrowserPayload)
	if !ok {
		return nil
	}
	if cmd != browserCommandGetBackupListReq && cmd != browserCommandRequestElection && cmd != browserCommandAnnouncementReq && cmd != browserCommandHostAnnouncement && cmd != browserCommandLocalMasterAnnounce && cmd != browserCommandDomainAnnouncement {
		return nil
	}
	netlog.Debug("[SMB][Browser] request cmd=0x%02x src=%q dst=%q mailslot=%q bytes=%d", cmd, d.Source.String(), d.Destination.String(), tx.MailslotName, len(framePayload))

	if cmd == browserCommandHostAnnouncement {
		host, err := unmarshalHostAnnouncementFrame(framePayload)
		if err != nil {
			return nil
		}
		s.noteBrowserServer(host.ServerName, host.ServerType)
		netlog.Debug("[SMB][Browser] observed host announcement server=%q type=0x%08x", host.ServerName, host.ServerType)
		return nil
	}

	if cmd == browserCommandLocalMasterAnnounce {
		master, err := unmarshalLocalMasterAnnouncementFrame(framePayload)
		if err != nil {
			return nil
		}
		s.noteBrowserServer(master.ServerName, master.ServerType|browserServerTypeMasterMask)
		netlog.Debug("[SMB][Browser] observed local master announcement server=%q type=0x%08x", master.ServerName, master.ServerType)
		return nil
	}

	if cmd == browserCommandDomainAnnouncement {
		da, err := unmarshalDomainAnnouncementFrame(framePayload)
		if err != nil {
			return nil
		}
		s.noteMachineGroup(da.MachineGroup, da.LocalMasterBrowserName)
		netlog.Debug("[SMB][Browser] observed domain announcement group=%q master=%q", da.MachineGroup, da.LocalMasterBrowserName)
		return nil
	}

	s.mu.Lock()
	sender := s.nbData
	server := s.opts.ServerName
	if server == "" {
		server = "CLASSICSTACK"
	}
	workgroup := s.opts.Workgroup
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	s.mu.Unlock()
	if sender == nil {
		return nil
	}

	if cmd == browserCommandAnnouncementReq {
		_, err := unmarshalAnnouncementRequestFrame(framePayload)
		if err != nil {
			return nil
		}
		announce := hostAnnouncementFrame{
			UpdateCount:         0x03,
			PeriodicityMS:       uint32(hostAnnouncementPeriod / time.Millisecond),
			ServerName:          server,
			OSVersionMajor:      0x04,
			OSVersionMinor:      0x00,
			ServerType:          browserServerTypeWorkstationMask,
			BrowserVersionMajor: hostAnnouncementVersionMajor,
			BrowserVersionMinor: hostAnnouncementVersionMinor,
			Signature:           browserSignature,
		}.MarshalBinary()
		response := browserMailslotTransaction{
			MailslotName:   browserMailslotBrowse,
			BrowserPayload: announce,
			TimeoutMS:      1000,
			Priority:       0,
			Class:          2,
		}.MarshalBinary()
		if ctx.Remote != (netbios.DatagramEndpoint{}) {
			netlog.Debug("[SMB][Browser] directed response cmd=0x01 src=%q dst=%q ipx=%x.%x:%02x%02x",
				server, d.Source.String(),
				ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
			return sender.SendDirectedDatagram(&netbiosproto.Datagram{
				Destination: d.Source,
				Source:      netbiosproto.NewName(server, netbiosproto.NameTypeFileServer),
				Payload:     response,
			}, ctx.Remote)
		}
		return sender.SendDatagram(&netbiosproto.Datagram{
			Destination: d.Source,
			Source:      netbiosproto.NewName(server, netbiosproto.NameTypeFileServer),
			Payload:     response,
		})
	}

	if cmd == browserCommandGetBackupListReq {
		s.mu.Lock()
		role := s.browserRole
		s.mu.Unlock()
		if role != browserRoleLocalMaster {
			netlog.Debug("[SMB][Browser] ignoring GetBackupListRequest while role=%d", role)
			return nil
		}
		request, err := unmarshalGetBackupListRequestFrame(framePayload)
		if err != nil {
			return nil
		}
		sourceName := backupListResponseSource(d.Destination, server, workgroup)
		backupServers := s.backupServerList(server)
		response := browserMailslotTransaction{
			MailslotName: browserMailslotBrowse,
			BrowserPayload: getBackupListResponseFrame{
				Token:         request.Token,
				BackupServers: backupServers,
			}.MarshalBinary(),
			TimeoutMS: 1000,
			Priority:  0,
			Class:     2,
		}.MarshalBinary()
		if ctx.Remote != (netbios.DatagramEndpoint{}) {
			netlog.Debug("[SMB][Browser] backup list entries=%d names=%v", len(backupServers), backupServers)
			netlog.Debug("[SMB][Browser] directed response cmd=0x0a src=%q<%02x> dst=%q ipx=%x.%x:%02x%02x token=0x%08x",
				sourceName.String(), sourceName.Type(), d.Source.String(),
				ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1],
				request.Token)
			return sender.SendDirectedDatagram(&netbiosproto.Datagram{
				Destination: d.Source,
				Source:      sourceName,
				Payload:     response,
			}, ctx.Remote)
		}
		netlog.Debug("[SMB][Browser] backup list entries=%d names=%s", len(backupServers), fmt.Sprintf("%v", backupServers))
		netlog.Debug("[SMB][Browser] response cmd=0x0a src=%q<%02x> dst=%q token=0x%08x",
			sourceName.String(), sourceName.Type(), d.Source.String(), request.Token)
		return sender.SendDatagram(&netbiosproto.Datagram{
			Destination: d.Source,
			Source:      sourceName,
			Payload:     response,
		})
	}

	request, err := unmarshalRequestElectionFrame(framePayload)
	if err != nil {
		return nil
	}
	if request.ServerName == "" {
		request.ServerName = d.Source.String()
	}
	local := s.localElectionFrame(server)
	cmp := compareElection(local, *request)
	netlog.Debug("[SMB][Browser] election request src=%q criteria=0x%08x uptime=%d server=%q localCriteria=0x%08x localUptime=%d cmp=%d",
		d.Source.String(), request.Criteria, request.Uptime, request.ServerName,
		local.Criteria, local.Uptime, cmp)

	if cmp < 0 {
		s.stopElectionLoop()
		s.mu.Lock()
		s.browserRole = browserRolePotential
		s.mu.Unlock()
		netlog.Info("[SMB][Browser] election lost to server=%q criteria=0x%08x uptime=%d", request.ServerName, request.Criteria, request.Uptime)
		return nil
	}
	// A tie (cmp == 0) usually means we just observed our own broadcast
	// echoed back. Stay silent — otherwise we ping-pong forever. Real
	// peers with identical criteria/uptime/name are vanishingly rare and
	// the MS-BRWS tie-break by name still resolves them on the next
	// election round.
	if cmp == 0 {
		netlog.Debug("[SMB][Browser] election tie ignored src=%q server=%q", d.Source.String(), request.ServerName)
		return nil
	}

	s.mu.Lock()
	originRole := s.browserRole
	s.mu.Unlock()
	if cmp > 0 {
		s.startElectionLoop(sender, server, workgroup, originRole)
	}

	netlog.Debug("[SMB][Browser] election transmit #1 src=%q dst=%q criteria=0x%08x uptime=%d",
		server,
		workgroup,
		local.Criteria,
		local.Uptime,
	)
	if err := s.sendElectionFrame(sender, server, workgroup, local); err != nil {
		return err
	}
	return nil
}

// shareEventSubscriber is the VFS bus subscriber installed by Start.
// It will (when implemented) match HostPath against share roots and
// invalidate any open handle whose backing path was renamed/deleted.
type shareEventSubscriber struct {
	shares []ShareConfig
}

// OnVFSEvent implements vfs.Subscriber.
func (s *shareEventSubscriber) OnVFSEvent(ev vfs.Event) {
	if ev.Origin == originSMB {
		return
	}
	// Stub: real invalidation lands with the open-handle map.
	_ = s.shares
}
