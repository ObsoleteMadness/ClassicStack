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
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/shortname"
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

	smbStatusSuccess      = 0x00000000
	smbStatusBadTID       = 0x00050002
	smbStatusNotSupported = 0xC00000BB

	// RAP-level (16-bit) error codes returned in the param Status field.
	rapStatusErrInvalidFunction = uint16(1)  // ERROR_INVALID_FUNCTION
	rapStatusErrReqNotAccepted  = uint16(71) // ERROR_REQ_NOT_ACCEP

	// SMB1 header field byte offsets (within the 32-byte SMB1 header).
	smbOffStatus = 5
	smbOffFlags  = 9
	smbOffTID    = 24
	smbOffUID    = 28

	// rapNetServerEnum2 is the RAP function code for NetServerEnum2.
	rapNetServerEnum2 = uint16(0x0068)

	// dialectNTLM is the NT LM 0.12 dialect string.
	dialectNTLM = "NT LM 0.12"
)

type browserMailslotTransaction struct {
	MailslotName   string
	BrowserPayload []byte
	Flags          uint16
	TimeoutMS      uint32
	Priority       uint16
	Class          uint16
}

func (t browserMailslotTransaction) MarshalBinary() []byte {
	mailslotName := t.MailslotName
	if mailslotName == "" {
		mailslotName = browserMailslotBrowse
	}
	nameField := append([]byte(mailslotName), 0)
	timeoutMS := t.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = 1000
	}
	class := t.Class
	if class == 0 {
		class = 2
	}
	out := make([]byte, browserTransactionDataOffset+len(t.BrowserPayload))
	copy(out[0:4], []byte{0xff, 'S', 'M', 'B'})
	out[4] = CommandTransaction
	out[32] = browserTransactionWordCount
	w := out[smbHeaderLen+1 : smbHeaderLen+1+browserTransactionWordsLen]
	binary.LittleEndian.PutUint16(w[0:2], 0)
	binary.LittleEndian.PutUint16(w[2:4], uint16(len(t.BrowserPayload)))
	binary.LittleEndian.PutUint16(w[4:6], 0)
	binary.LittleEndian.PutUint16(w[6:8], 0)
	w[8] = 0
	w[9] = 0
	binary.LittleEndian.PutUint16(w[10:12], t.Flags)
	binary.LittleEndian.PutUint32(w[12:16], timeoutMS)
	binary.LittleEndian.PutUint16(w[16:18], 0)
	binary.LittleEndian.PutUint16(w[18:20], 0)
	binary.LittleEndian.PutUint16(w[20:22], 0)
	binary.LittleEndian.PutUint16(w[22:24], uint16(len(t.BrowserPayload)))
	binary.LittleEndian.PutUint16(w[24:26], browserTransactionDataOffset)
	w[26] = 3
	w[27] = 0
	binary.LittleEndian.PutUint16(w[28:30], 1)
	binary.LittleEndian.PutUint16(w[30:32], t.Priority)
	binary.LittleEndian.PutUint16(w[32:34], class)
	binary.LittleEndian.PutUint16(out[browserTransactionByteOffset:browserTransactionByteOffset+2], uint16(len(nameField)+len(t.BrowserPayload)))
	copy(out[browserTransactionByteOffset+2:browserTransactionByteOffset+2+len(nameField)], nameField)
	copy(out[browserTransactionDataOffset:], t.BrowserPayload)
	return out
}

func unmarshalBrowserMailslotTransaction(payload []byte) (*browserMailslotTransaction, error) {
	if len(payload) < browserTransactionByteOffset+2 || string(payload[0:4]) != "\xffSMB" {
		return nil, errors.New("smb: invalid transaction header")
	}
	if payload[4] != CommandTransaction || payload[32] != browserTransactionWordCount {
		return nil, errors.New("smb: not a mailslot transaction")
	}
	w := payload[33:67]
	dataCount := int(binary.LittleEndian.Uint16(w[22:24]))
	dataOffset := int(binary.LittleEndian.Uint16(w[24:26]))
	if dataCount == 0 || dataOffset < browserTransactionByteOffset+2 || dataOffset > len(payload) || dataOffset+dataCount > len(payload) {
		return nil, errors.New("smb: invalid transaction data window")
	}
	byteCount := int(binary.LittleEndian.Uint16(payload[browserTransactionByteOffset : browserTransactionByteOffset+2]))
	byteStart := browserTransactionByteOffset + 2
	byteEnd := byteStart + byteCount
	if byteEnd > len(payload) {
		return nil, errors.New("smb: invalid byte count")
	}
	nameEnd := bytes.IndexByte(payload[byteStart:dataOffset], 0)
	if nameEnd < 0 {
		return nil, errors.New("smb: missing mailslot terminator")
	}
	name := string(payload[byteStart : byteStart+nameEnd])
	return &browserMailslotTransaction{
		MailslotName:   name,
		BrowserPayload: append([]byte(nil), payload[dataOffset:dataOffset+dataCount]...),
		Flags:          binary.LittleEndian.Uint16(w[10:12]),
		TimeoutMS:      binary.LittleEndian.Uint32(w[12:16]),
		Priority:       binary.LittleEndian.Uint16(w[30:32]),
		Class:          binary.LittleEndian.Uint16(w[32:34]),
	}, nil
}

type hostAnnouncementFrame struct {
	UpdateCount         uint8
	PeriodicityMS       uint32
	ServerName          string
	OSVersionMajor      uint8
	OSVersionMinor      uint8
	ServerType          uint32
	BrowserVersionMajor uint8
	BrowserVersionMinor uint8
	Signature           uint16
	Comment             string
}

type localMasterAnnouncementFrame struct {
	UpdateCount               uint8
	PeriodicityMS             uint32
	ServerName                string
	OSVersionMajor            uint8
	OSVersionMinor            uint8
	ServerType                uint32
	BrowserConfigVersionMajor uint8
	BrowserConfigVersionMinor uint8
	Signature                 uint16
	Comment                   string
}

func (f hostAnnouncementFrame) MarshalBinary() []byte {
	out := make([]byte, 33)
	out[0] = browserCommandHostAnnouncement
	out[1] = f.UpdateCount
	binary.LittleEndian.PutUint32(out[2:6], f.PeriodicityMS)
	serverName := fixedBrowserName(f.ServerName)
	copy(out[6:22], serverName[:])
	out[22] = f.OSVersionMajor
	out[23] = f.OSVersionMinor
	binary.LittleEndian.PutUint32(out[24:28], f.ServerType)
	out[28] = f.BrowserVersionMajor
	out[29] = f.BrowserVersionMinor
	binary.LittleEndian.PutUint16(out[30:32], f.Signature)
	comment := strings.TrimSpace(f.Comment)
	if len(comment) > 42 {
		comment = comment[:42]
	}
	if comment != "" {
		return append(out[:32], append([]byte(comment), 0)...)
	}
	out[32] = 0
	return out
}

func unmarshalHostAnnouncementFrame(payload []byte) (*hostAnnouncementFrame, error) {
	if len(payload) < 33 || payload[0] != browserCommandHostAnnouncement {
		return nil, errors.New("smb: invalid host announcement frame")
	}
	comment := ""
	if len(payload) > 32 {
		comment = parseBrowserString(payload[32:])
	}
	return &hostAnnouncementFrame{
		UpdateCount:         payload[1],
		PeriodicityMS:       binary.LittleEndian.Uint32(payload[2:6]),
		ServerName:          parseBrowserString(payload[6:22]),
		OSVersionMajor:      payload[22],
		OSVersionMinor:      payload[23],
		ServerType:          binary.LittleEndian.Uint32(payload[24:28]),
		BrowserVersionMajor: payload[28],
		BrowserVersionMinor: payload[29],
		Signature:           binary.LittleEndian.Uint16(payload[30:32]),
		Comment:             comment,
	}, nil
}

func (f localMasterAnnouncementFrame) MarshalBinary() []byte {
	out := make([]byte, 33)
	out[0] = browserCommandLocalMasterAnnounce
	out[1] = f.UpdateCount
	binary.LittleEndian.PutUint32(out[2:6], f.PeriodicityMS)
	serverName := fixedBrowserName(f.ServerName)
	copy(out[6:22], serverName[:])
	out[22] = f.OSVersionMajor
	out[23] = f.OSVersionMinor
	binary.LittleEndian.PutUint32(out[24:28], f.ServerType)
	out[28] = f.BrowserConfigVersionMajor
	out[29] = f.BrowserConfigVersionMinor
	binary.LittleEndian.PutUint16(out[30:32], f.Signature)
	comment := strings.TrimSpace(f.Comment)
	if len(comment) > 42 {
		comment = comment[:42]
	}
	if comment != "" {
		return append(out[:32], append([]byte(comment), 0)...)
	}
	out[32] = 0
	return out
}

func unmarshalLocalMasterAnnouncementFrame(payload []byte) (*localMasterAnnouncementFrame, error) {
	if len(payload) < 33 || payload[0] != browserCommandLocalMasterAnnounce {
		return nil, errors.New("smb: invalid local-master-announcement frame")
	}
	comment := ""
	if len(payload) > 32 {
		comment = parseBrowserString(payload[32:])
	}
	return &localMasterAnnouncementFrame{
		UpdateCount:               payload[1],
		PeriodicityMS:             binary.LittleEndian.Uint32(payload[2:6]),
		ServerName:                parseBrowserString(payload[6:22]),
		OSVersionMajor:            payload[22],
		OSVersionMinor:            payload[23],
		ServerType:                binary.LittleEndian.Uint32(payload[24:28]),
		BrowserConfigVersionMajor: payload[28],
		BrowserConfigVersionMinor: payload[29],
		Signature:                 binary.LittleEndian.Uint16(payload[30:32]),
		Comment:                   comment,
	}, nil
}

type requestElectionFrame struct {
	Version    uint8
	Criteria   uint32
	Uptime     uint32
	Reserved   uint32
	ServerName string
}

func (f requestElectionFrame) MarshalBinary() []byte {
	out := make([]byte, 14)
	out[0] = browserCommandRequestElection
	out[1] = f.Version
	binary.LittleEndian.PutUint32(out[2:6], f.Criteria)
	binary.LittleEndian.PutUint32(out[6:10], f.Uptime)
	binary.LittleEndian.PutUint32(out[10:14], f.Reserved)
	return appendBrowserName(out, f.ServerName)
}

func unmarshalRequestElectionFrame(payload []byte) (*requestElectionFrame, error) {
	if len(payload) < 15 || payload[0] != browserCommandRequestElection {
		return nil, errors.New("smb: invalid election frame")
	}
	return &requestElectionFrame{
		Version:    payload[1],
		Criteria:   binary.LittleEndian.Uint32(payload[2:6]),
		Uptime:     binary.LittleEndian.Uint32(payload[6:10]),
		Reserved:   binary.LittleEndian.Uint32(payload[10:14]),
		ServerName: parseBrowserString(payload[14:]),
	}, nil
}

type getBackupListRequestFrame struct {
	RequestedCount uint8
	Token          uint32
}

func (f getBackupListRequestFrame) MarshalBinary() []byte {
	out := make([]byte, 6)
	out[0] = browserCommandGetBackupListReq
	out[1] = f.RequestedCount
	binary.LittleEndian.PutUint32(out[2:6], f.Token)
	return out
}

func unmarshalGetBackupListRequestFrame(payload []byte) (*getBackupListRequestFrame, error) {
	if len(payload) < 6 || payload[0] != browserCommandGetBackupListReq {
		return nil, errors.New("smb: invalid backup-list request frame")
	}
	return &getBackupListRequestFrame{
		RequestedCount: payload[1],
		Token:          binary.LittleEndian.Uint32(payload[2:6]),
	}, nil
}

type getBackupListResponseFrame struct {
	Token         uint32
	BackupServers []string
}

type announcementRequestFrame struct {
	Reserved     uint8
	ResponseName string
}

func unmarshalAnnouncementRequestFrame(payload []byte) (*announcementRequestFrame, error) {
	if len(payload) < 2 || payload[0] != browserCommandAnnouncementReq {
		return nil, errors.New("smb: invalid announcement-request frame")
	}
	responseName := ""
	if len(payload) > 2 {
		responseName = parseBrowserString(payload[2:])
	}
	return &announcementRequestFrame{
		Reserved:     payload[1],
		ResponseName: responseName,
	}, nil
}

func (f getBackupListResponseFrame) MarshalBinary() []byte {
	out := make([]byte, 6)
	out[0] = browserCommandGetBackupListResp
	out[1] = uint8(len(f.BackupServers))
	binary.LittleEndian.PutUint32(out[2:6], f.Token)
	for _, server := range f.BackupServers {
		out = appendBrowserName(out, server)
	}
	return out
}

func unmarshalGetBackupListResponseFrame(payload []byte) (*getBackupListResponseFrame, error) {
	if len(payload) < 6 || payload[0] != browserCommandGetBackupListResp {
		return nil, errors.New("smb: invalid backup-list response frame")
	}
	count := int(payload[1])
	servers := make([]string, 0, count)
	rest := payload[6:]
	for len(rest) > 0 && len(servers) < count {
		idx := bytes.IndexByte(rest, 0)
		if idx < 0 {
			return nil, errors.New("smb: unterminated backup-list server name")
		}
		servers = append(servers, parseBrowserString(rest[:idx+1]))
		rest = rest[idx+1:]
	}
	if len(servers) != count {
		return nil, errors.New("smb: backup-list count mismatch")
	}
	return &getBackupListResponseFrame{
		Token:         binary.LittleEndian.Uint32(payload[2:6]),
		BackupServers: servers,
	}, nil
}

type domainAnnouncementFrame struct {
	Periodicity            uint32
	MachineGroup           string
	ServerType             uint32
	LocalMasterBrowserName string
}

// unmarshalDomainAnnouncementFrame parses a DomainAnnouncement browser frame (opcode 0x0C).
// Layout per MS-BRWS §2.2.7: opcode(1)+UpdateCount(1)+Periodicity(4)+MachineGroup(16)+
// BrowserConfigVersionMajor(1)+BrowserConfigVersionMinor(1)+ServerType(4)+
// BrowserVersionMajor(1)+BrowserVersionMinor(1)+Signature(2)+LocalMasterBrowserName(variable).
func unmarshalDomainAnnouncementFrame(payload []byte) (*domainAnnouncementFrame, error) {
	const fixedLen = 32
	if len(payload) < fixedLen+1 || payload[0] != browserCommandDomainAnnouncement {
		return nil, errors.New("smb: invalid domain announcement frame")
	}
	periodicity := binary.LittleEndian.Uint32(payload[2:6])
	machineGroup := parseBrowserString(payload[6:22])
	serverType := binary.LittleEndian.Uint32(payload[24:28])
	masterName := parseBrowserString(payload[fixedLen:])
	return &domainAnnouncementFrame{
		Periodicity:            periodicity,
		MachineGroup:           machineGroup,
		ServerType:             serverType,
		LocalMasterBrowserName: masterName,
	}, nil
}

func normalizeBrowserName(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if len(upper) > 15 {
		upper = upper[:15]
	}
	return upper
}

func fixedBrowserName(name string) [16]byte {
	var out [16]byte
	normalized := normalizeBrowserName(name)
	copy(out[:], normalized)
	return out
}

func appendBrowserName(dst []byte, name string) []byte {
	normalized := normalizeBrowserName(name)
	dst = append(dst, normalized...)
	return append(dst, 0)
}

func parseBrowserString(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx >= 0 {
		b = b[:idx]
	}
	return strings.TrimRight(string(b), "\x00")
}

func backupListResponseSource(requestDst netbiosproto.Name, server, workgroup string) netbiosproto.Name {
	// Win9x clients often address GetBackupListRequest to <workgroup><1D>.
	// Mirror that identity in replies so browser clients can accept the source
	// as the local master browser for the machine group.
	if requestDst.Type() == browserNameTypeMasterBrowser && strings.EqualFold(requestDst.String(), workgroup) {
		return netbiosproto.NewName(workgroup, browserNameTypeMasterBrowser)
	}
	return netbiosproto.NewName(server, netbiosproto.NameTypeFileServer)
}

func isBrowserCommandByte(b byte) bool {
	switch b {
	case browserCommandHostAnnouncement,
		browserCommandAnnouncementReq,
		browserCommandRequestElection,
		browserCommandGetBackupListReq,
		browserCommandGetBackupListResp,
		browserCommandLocalMasterAnnounce:
		return true
	default:
		return false
	}
}

func unwrapBrowserPayload(payload []byte) (cmd byte, frame []byte, ok bool) {
	if len(payload) == 0 {
		return 0, nil, false
	}
	// Legacy Win9x browser requests can include a two-byte preamble
	// before the browser opcode (for example 0x01 0x03 0x09 or
	// 0x0f 0x06 0x08). Prefer this form when detected.
	if len(payload) >= 3 && isBrowserCommandByte(payload[2]) {
		if (payload[0] == 0x01 && payload[1] == 0x03) || (payload[0] == 0x0f && payload[1] == 0x06) {
			return payload[2], payload[2:], true
		}
	}
	if isBrowserCommandByte(payload[0]) {
		return payload[0], payload, true
	}
	if len(payload) >= 3 && isBrowserCommandByte(payload[2]) {
		return payload[2], payload[2:], true
	}
	return 0, nil, false
}

// ServerOptions configures the SMB service.
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
	Shortname shortname.Mapper
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
func (s *Service) HandleSession(_ *netbiosproto.SessionPacket) error { return ErrNotImplemented }

// HandleSessionContext implements netbios.ContextualSessionHandler.
// It handles the minimal SMB1 session sequence needed for Network
// Neighbourhood enumeration: NegotiateProtocol (0x72), SessionSetupAndX
// (0x73), TreeConnectAndX (0x75), and LANMAN Transaction requests on
// \PIPE\LANMAN (NetServerEnum2). All other commands return
// STATUS_NOT_SUPPORTED.
func (s *Service) HandleSessionContext(packet *netbiosproto.SessionPacket, ctx netbios.SessionContext) (*netbiosproto.SessionPacket, error) {
	if packet == nil || len(packet.Payload) < smbHeaderLen || string(packet.Payload[0:4]) != "\xffSMB" {
		return nil, nil
	}

	connID := connKeyFromSession(ctx)
	conn := s.ensureConn(connID)

	server := s.opts.ServerName
	if server == "" {
		server = "CLASSICSTACK"
	}
	workgroup := s.opts.Workgroup
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}

	cmd := packet.Payload[4]
	var respPayload []byte

	switch cmd {
	case CommandNegotiate:
		netlog.Debug("[SMB][Session] negotiate src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = buildNegotiateResponse(packet.Payload, workgroup)

	case CommandSessionSetupAndX:
		netlog.Debug("[SMB][Session] session-setup src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		conn.mu.Lock()
		if conn.uid == 0 {
			conn.uid = s.allocUID()
		}
		uid := conn.uid
		conn.mu.Unlock()
		respPayload = buildSessionSetupResponse(packet.Payload, uid)

	case CommandTreeConnectAndX:
		netlog.Debug("[SMB][Session] tree-connect src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleTreeConnectAndX(packet.Payload, conn)

	case CommandEcho:
		netlog.Debug("[SMB][Session] echo src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		if !isValidEchoTID(packet.Payload, conn) {
			respPayload = buildSMBErrorResponse(packet.Payload, smbStatusBadTID)
			break
		}
		respPayload = buildEchoResponse(packet.Payload)

	case CommandTreeDisconnect:
		netlog.Debug("[SMB][Session] tree-disconnect src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		conn.mu.Lock()
		if len(packet.Payload) >= smbHeaderLen {
			tid := binary.LittleEndian.Uint16(packet.Payload[smbOffTID : smbOffTID+2])
			delete(conn.tids, tid)
		}
		conn.mu.Unlock()
		respPayload = buildSimpleSuccessResponse(packet.Payload)

	case CommandLogoffAndX:
		netlog.Debug("[SMB][Session] logoff src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		s.dropConn(connID)
		respPayload = buildSimpleSuccessResponse(packet.Payload)

	case CommandTransaction:
		if !isLANMANTransactionRequest(packet.Payload) {
			netlog.Debug("[SMB][Session] unsupported transaction src=%x.%x:%02x%02x",
				ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
			respPayload = buildSMBErrorResponse(packet.Payload, smbStatusNotSupported)
		} else {
			fc, ok := parseLANMANFunctionCode(packet.Payload)
			if ok && fc == rapNetServerEnum2 {
				serverType, _ := parseNetServerEnum2ServerType(packet.Payload)
				reqDomain, _ := parseNetServerEnum2Domain(packet.Payload)
				netlog.Debug("[SMB][Session] NetServerEnum2 src=%x.%x:%02x%02x serverType=%#x domain=%q",
					ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1], serverType, reqDomain)
				entries, rapStatus := s.netServerEnum2Entries(serverType, workgroup, reqDomain)
				if rapStatus != 0 {
					respPayload = buildNetServerEnum2RAPErrorResponse(packet.Payload, rapStatus)
				} else {
					respPayload = buildNetServerEnum2Response(packet.Payload, entries)
				}
			} else {
				netlog.Debug("[SMB][Session] LANMAN fc=%#x src=%x.%x:%02x%02x",
					fc, ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
				respPayload = buildSMBTransactionEmptySuccess(packet.Payload)
			}
		}

	case CommandQueryInformationDisk:
		netlog.Debug("[SMB][Session] query-information-disk src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleQueryInformationDisk(packet.Payload, conn)

	case CommandCheckDirectory:
		netlog.Debug("[SMB][Session] check-directory src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleCheckDirectory(packet.Payload, conn)

	case CommandSearch:
		netlog.Debug("[SMB][Session] search src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleSearch(packet.Payload, conn)

	case CommandOpenAndX:
		netlog.Debug("[SMB][Session] open-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleOpenAndX(packet.Payload, conn)

	case CommandReadAndX:
		netlog.Debug("[SMB][Session] read-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleReadAndX(packet.Payload, conn)

	case CommandWriteAndX:
		netlog.Debug("[SMB][Session] write-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleWriteAndX(packet.Payload, conn)

	case CommandClose:
		netlog.Debug("[SMB][Session] close src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleClose(packet.Payload, conn)

	default:
		netlog.Debug("[SMB][Session] unsupported command=0x%02x src=%x.%x:%02x%02x",
			cmd, ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = buildSMBErrorResponse(packet.Payload, smbStatusNotSupported)
	}

	if respPayload == nil {
		return nil, nil
	}
	return &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: respPayload,
	}, nil
}

func isLANMANTransactionRequest(req []byte) bool {
	bytesArea, ok := transactionBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return false
	}
	return bytes.Contains(bytes.ToUpper(bytesArea), []byte("\\PIPE\\LANMAN"))
}

// transactionBytesArea returns the SMB_COM_TRANSACTION bytes area,
// regardless of request word-count shape.
func transactionBytesArea(req []byte) ([]byte, bool) {
	if len(req) < smbHeaderLen+3 || string(req[0:4]) != "\xffSMB" || req[4] != CommandTransaction {
		return nil, false
	}
	wct := int(req[smbHeaderLen])
	bytesOffset := smbHeaderLen + 1 + (wct * 2)
	if bytesOffset+2 > len(req) {
		return nil, false
	}
	byteCount := int(binary.LittleEndian.Uint16(req[bytesOffset : bytesOffset+2]))
	if byteCount < 0 || bytesOffset+2+byteCount > len(req) {
		return nil, false
	}
	return req[bytesOffset+2 : bytesOffset+2+byteCount], true
}

func buildSMBErrorResponse(req []byte, status uint32) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+3)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[5:9], status)
	out[9] = req[9] | 0x80
	out[32] = 0
	binary.LittleEndian.PutUint16(out[33:35], 0)
	return out
}

// findNegotiateDialect scans the dialect list in a SMB_COM_NEGOTIATE
// request and returns the 0-based index of the named dialect, or -1
// if not found.
func findNegotiateDialect(req []byte, name string) int {
	if len(req) < smbHeaderLen+3 {
		return -1
	}
	byteCount := int(binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3]))
	if len(req) < smbHeaderLen+3+byteCount {
		return -1
	}
	rest := req[smbHeaderLen+3 : smbHeaderLen+3+byteCount]
	idx := 0
	for len(rest) >= 2 {
		if rest[0] != 0x02 {
			break
		}
		rest = rest[1:]
		nul := bytes.IndexByte(rest, 0)
		if nul < 0 {
			break
		}
		if string(rest[:nul]) == name {
			return idx
		}
		rest = rest[nul+1:]
		idx++
	}
	return -1
}

// buildNegotiateResponse constructs an SMB_COM_NEGOTIATE response
// accepting the NT LM 0.12 dialect (WCT=17). SecurityMode is set to
// user-level without challenge so Win98 can send plain-text credentials
// and proceed to the guest session path.
func buildNegotiateResponse(req []byte, workgroup string) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	dialectIdx := findNegotiateDialect(req, dialectNTLM)
	if dialectIdx < 0 {
		dialectIdx = 0
	}
	domain := normalizeBrowserName(workgroup)
	domainBytes := append([]byte(domain), 0)

	paramLen := 34 // 17 words
	out := make([]byte, smbHeaderLen+1+paramLen+2+len(domainBytes))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 17 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(dialectIdx)) // DialectIndex
	w[2] = 0x01                                               // SecurityMode: user-level, no encryption
	binary.LittleEndian.PutUint16(w[3:5], 50)                 // MaxMpxCount
	binary.LittleEndian.PutUint16(w[5:7], 1)                  // MaxNumberVcs
	binary.LittleEndian.PutUint32(w[7:11], 0x4000)            // MaxBufferSize
	binary.LittleEndian.PutUint32(w[11:15], 0)                // MaxRawSize
	binary.LittleEndian.PutUint32(w[15:19], 0)                // SessionKey
	binary.LittleEndian.PutUint32(w[19:23], 0)                // Capabilities
	binary.LittleEndian.PutUint32(w[23:27], 0)                // SystemTimeLow
	binary.LittleEndian.PutUint32(w[27:31], 0)                // SystemTimeHigh
	binary.LittleEndian.PutUint16(w[31:33], 0)                // ServerTimeZone
	w[33] = 0                                                 // EncryptionKeyLength = 0 (no challenge)
	binary.LittleEndian.PutUint16(w[34:36], uint16(len(domainBytes)))
	copy(w[36:], domainBytes)
	return out
}

// buildSessionSetupResponse constructs an SMB_COM_SESSION_SETUP_ANDX
// response granting a guest session (UID=1, Action=0x0001).
func buildSessionSetupResponse(req []byte, uid uint16) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	// WCT=3: AndXCommand(1b)+AndXReserved(1b)+AndXOffset(2b)+Action(2b).
	// ByteCount=2: empty NativeOS and NativeLM strings (two NULs).
	out := make([]byte, smbHeaderLen+1+6+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	binary.LittleEndian.PutUint16(out[smbOffUID:smbOffUID+2], uid) // guest session UID
	out[smbHeaderLen] = 3                                          // WCT
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF                                   // AndXCommand = no chaining
	w[1] = 0x00                                   // AndXReserved
	binary.LittleEndian.PutUint16(w[2:4], 0)      // AndXOffset
	binary.LittleEndian.PutUint16(w[4:6], 0x0001) // Action = guest logon
	binary.LittleEndian.PutUint16(w[6:8], 2)      // ByteCount
	w[8] = 0x00                                   // NativeOS = ""
	w[9] = 0x00                                   // NativeLM = ""
	return out
}

// buildTreeConnectResponse constructs an SMB_COM_TREE_CONNECT_ANDX
// response assigning TID=1 with IPC$ service type.
func buildTreeConnectResponse(req []byte) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	service := []byte("IPC\x00")
	nativeFS := []byte("\x00")
	byteCount := len(service) + len(nativeFS)
	// WCT=3: AndXCommand(1b)+AndXReserved(1b)+AndXOffset(2b)+OptionalSupport(2b).
	out := make([]byte, smbHeaderLen+1+6+2+byteCount)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], 1) // TID = 1
	out[smbHeaderLen] = 3                                        // WCT
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF                              // AndXCommand = no chaining
	w[1] = 0x00                              // AndXReserved
	binary.LittleEndian.PutUint16(w[2:4], 0) // AndXOffset
	binary.LittleEndian.PutUint16(w[4:6], 0) // OptionalSupport
	binary.LittleEndian.PutUint16(w[6:8], uint16(byteCount))
	copy(w[8:], service)
	copy(w[8+len(service):], nativeFS)
	return out
}

// buildEchoResponse constructs an SMB_COM_ECHO response that mirrors
// the request body and sets SequenceNumber to 1.
func buildEchoResponse(req []byte) []byte {
	if len(req) < smbHeaderLen+5 || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	if req[smbHeaderLen] != 1 {
		return nil
	}
	echoCount := binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3])
	if echoCount == 0 {
		return nil
	}
	byteCount := int(binary.LittleEndian.Uint16(req[smbHeaderLen+3 : smbHeaderLen+5]))
	if byteCount < 0 || len(req) < smbHeaderLen+5+byteCount {
		return nil
	}
	data := req[smbHeaderLen+5 : smbHeaderLen+5+byteCount]
	out := make([]byte, smbHeaderLen+1+2+2+len(data))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1 // WCT
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 1)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+3:smbHeaderLen+5], uint16(len(data)))
	copy(out[smbHeaderLen+5:], data)
	return out
}

func isValidEchoTID(req []byte, conn *connState) bool {
	if len(req) < smbHeaderLen {
		return false
	}
	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])
	if tid == 0xFFFF || tid == 1 {
		return true
	}
	if conn == nil {
		return false
	}
	conn.mu.Lock()
	_, ok := conn.tids[tid]
	conn.mu.Unlock()
	return ok
}

func (s *Service) handleTreeConnectAndX(req []byte, conn *connState) []byte {
	if conn == nil {
		return buildTreeConnectResponse(req)
	}
	shareName, ok := parseTreeConnectShareName(req)
	if !ok {
		return buildTreeConnectResponse(req)
	}

	normalized := normalizeBrowserName(shareName)
	s.mu.Lock()
	shareIdx, found := s.shareNameToIndex[normalized]
	s.mu.Unlock()
	if !found {
		return buildTreeConnectResponse(req)
	}

	conn.mu.Lock()
	conn.nextTID++
	tid := conn.nextTID
	if tid == 0 {
		conn.nextTID++
		tid = conn.nextTID
	}
	conn.tids[tid] = treeSlot{shareIdx: shareIdx}
	conn.mu.Unlock()

	return buildTreeConnectResponseWithTID(req, tid)
}

// handleQueryInformationDisk (0x80) reports disk geometry and free space
// for the share associated with the request's TID.
func (s *Service) handleQueryInformationDisk(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])

	conn.mu.Lock()
	slot, ok := conn.tids[tid]
	conn.mu.Unlock()
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	s.mu.Lock()
	fs, ok := s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fs == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	totalBytes, freeBytes, err := fs.DiskUsage("")
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	return buildQueryInformationDiskResponse(req, totalBytes, freeBytes)
}

func buildTreeConnectResponseWithTID(req []byte, tid uint16) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	service := []byte("A:\x00")
	nativeFS := []byte("\x00")
	byteCount := len(service) + len(nativeFS)
	out := make([]byte, smbHeaderLen+1+6+2+byteCount)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	binary.LittleEndian.PutUint16(out[smbOffTID:smbOffTID+2], tid)
	out[smbHeaderLen] = 3
	w := out[smbHeaderLen+1:]
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)
	binary.LittleEndian.PutUint16(w[4:6], 0)
	binary.LittleEndian.PutUint16(w[6:8], uint16(byteCount))
	copy(w[8:], service)
	copy(w[8+len(service):], nativeFS)
	return out
}

// handleCheckDirectory (0x10) verifies a path is a directory.
func (s *Service) handleCheckDirectory(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])

	conn.mu.Lock()
	slot, ok := conn.tids[tid]
	conn.mu.Unlock()
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	s.mu.Lock()
	fs, ok := s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fs == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	info, err := fs.Stat(path)
	if err != nil {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	if !info.IsDir() {
		return buildSMBErrorResponse(req, 0xC0000103) // STATUS_NOT_A_DIRECTORY
	}

	return buildSimpleSuccessResponse(req)
}

// handleSearch (0x81) performs directory enumeration with pattern matching.
// Returns entries in DOS 8.3 format suitable for Win9x/DOS clients.
func (s *Service) handleSearch(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+11 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])

	conn.mu.Lock()
	slot, ok := conn.tids[tid]
	conn.mu.Unlock()
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	s.mu.Lock()
	fs, ok := s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fs == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	// Parse request parameters
	wct := int(req[smbHeaderLen])
	if wct < 2 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	attrs := binary.LittleEndian.Uint16(req[smbHeaderLen+1 : smbHeaderLen+3])
	pattern, ok := parseSMBPath(req)
	if !ok || pattern == "" {
		pattern = "*"
	}

	// For now, do a simple search - don't handle resume keys yet
	// Get the directory part of the pattern
	lastSlash := strings.LastIndex(pattern, "\\")
	var dirPath, filePattern string
	if lastSlash >= 0 {
		dirPath = pattern[:lastSlash]
		filePattern = pattern[lastSlash+1:]
	} else {
		dirPath = ""
		filePattern = pattern
	}

	// Read directory
	entries, err := fs.ReadDir(dirPath)
	if err != nil {
		return buildSearchEmptyResponse(req)
	}

	// Filter and match entries
	var results []searchResultEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Filter by attributes
		if !matchesSearchAttrs(info, attrs) {
			continue
		}

		// Match filename pattern (simple: check if matches *.* or specific name)
		if !matchesPattern(entry.Name(), filePattern) {
			continue
		}

		results = append(results, searchResultEntry{
			name:       entry.Name(),
			size:       info.Size(),
			modTime:    info.ModTime(),
			isDir:      info.IsDir(),
			attributes: getSearchAttrs(info),
		})

		if len(results) >= 10 {
			break
		}
	}

	return buildSearchResponse(req, results)
}

// handleOpenAndX (0x2D) opens or creates a file, returning a file handle.
func (s *Service) handleOpenAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+15 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	tid := binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])

	conn.mu.Lock()
	slot, ok := conn.tids[tid]
	conn.mu.Unlock()
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	s.mu.Lock()
	fs, ok := s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fs == nil {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 15 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	desiredAccess := binary.LittleEndian.Uint16(w[0:2])
	searchAttrs := binary.LittleEndian.Uint16(w[2:4])
	fileAttrs := binary.LittleEndian.Uint16(w[4:6])
	createTime := binary.LittleEndian.Uint32(w[6:10])
	openFunction := binary.LittleEndian.Uint16(w[10:12])

	_ = desiredAccess
	_ = searchAttrs
	_ = createTime

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	// Determine open mode
	var file vfs.File
	var err error

	// OPEN_FUNCTION: bits 0-3: action, bits 4-7: mode
	mode := openFunction >> 4
	_ = openFunction & 0x0F // action (unused for now)

	// Try to open existing file / create new
	if mode == 1 {
		// OPEN_IF_EXISTS
		file, err = fs.OpenFile(path, 0) // Read mode
	} else if mode == 2 {
		// OPEN_EXCLUSIVE
		file, err = fs.CreateFile(path)
	} else {
		// Default: try to open, create if not found
		file, err = fs.OpenFile(path, 0)
		if err != nil {
			file, err = fs.CreateFile(path)
		}
	}

	if err != nil {
		return buildSMBErrorResponse(req, 0xC000007F) // STATUS_OBJECT_NAME_NOT_FOUND
	}

	// Get file info
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Allocate FID
	conn.mu.Lock()
	conn.nextFID++
	fid := conn.nextFID
	if fid == 0 {
		conn.nextFID++
		fid = conn.nextFID
	}
	conn.fids[fid] = &fileHandle{
		file:     file,
		path:     path,
		tid:      tid,
		writable: (fileAttrs & FileAttributeReadOnly) == 0,
	}
	conn.mu.Unlock()

	return buildOpenAndXResponse(req, fid, info, fileAttrs)
}

// handleReadAndX (0x2E) reads data from an open file.
func (s *Service) handleReadAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+11 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 5 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[2:4])
	offset := binary.LittleEndian.Uint32(w[4:8])
	maxCount := binary.LittleEndian.Uint16(w[8:10])
	_ = binary.LittleEndian.Uint16(w[10:12]) // minCount (unused)

	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Clamp read size
	if maxCount > 4096 {
		maxCount = 4096
	}

	// Read from file
	data := make([]byte, maxCount)
	n, err := handle.file.ReadAt(data, int64(offset))
	if err != nil && err.Error() != "EOF" {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	data = data[:n]
	return buildReadAndXResponse(req, data)
}

// handleWriteAndX (0x2F) writes data to an open file.
func (s *Service) handleWriteAndX(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+13 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 6 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[2:4])
	offset := binary.LittleEndian.Uint32(w[4:8])
	_ = binary.LittleEndian.Uint16(w[12:14]) // writeMode (unused)

	// Get data from bytes area
	bytesArea, ok := smbBytesArea(req)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Look up file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	conn.mu.Unlock()
	if !ok || handle == nil || handle.file == nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	if !handle.writable {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Write to file
	n, err := handle.file.WriteAt(bytesArea, int64(offset))
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	return buildWriteAndXResponse(req, uint16(n))
}

// handleClose (0x04) closes an open file and releases the file handle.
func (s *Service) handleClose(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+7 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	// Parse request
	wct := int(req[smbHeaderLen])
	if wct < 3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	w := req[smbHeaderLen+1:]
	fid := binary.LittleEndian.Uint16(w[0:2])
	// lastWriteTime := binary.LittleEndian.Uint32(w[2:6]) // unused

	// Look up and close file handle
	conn.mu.Lock()
	handle, ok := conn.fids[fid]
	if ok {
		if handle != nil && handle.file != nil {
			handle.file.Close()
		}
		delete(conn.fids, fid)
	}
	conn.mu.Unlock()

	return buildSimpleSuccessResponse(req)
}

func buildWriteAndXResponse(req []byte, count uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+(6*2)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 6 // WCT
	w := out[smbHeaderLen+1:]

	// AndXCommand, AndXReserved, AndXOffset
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)

	// Count (bytes written)
	binary.LittleEndian.PutUint16(w[4:6], count)

	// Remaining (bytes left in transaction)
	binary.LittleEndian.PutUint16(w[6:8], 0)

	// Reserved
	binary.LittleEndian.PutUint32(w[8:12], 0)

	// ByteCount = 0
	binary.LittleEndian.PutUint16(w[12:14], 0)

	return out
}

func buildReadAndXResponse(req []byte, data []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+(12*2)+2+len(data))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 12 // WCT
	w := out[smbHeaderLen+1:]

	// AndXCommand, AndXReserved, AndXOffset
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)

	// Remaining (words available for next command)
	binary.LittleEndian.PutUint16(w[4:6], 0)

	// DataCompactionMode
	binary.LittleEndian.PutUint16(w[6:8], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[8:10], 0)

	// DataLength
	binary.LittleEndian.PutUint16(w[10:12], uint16(len(data)))

	// DataOffset relative to SMB header
	dataOffset := smbHeaderLen + 1 + (12 * 2) + 2
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataOffset))

	// Reserved
	binary.LittleEndian.PutUint16(w[14:16], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[16:18], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[18:20], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[20:22], 0)

	// ByteCount
	binary.LittleEndian.PutUint16(w[22:24], uint16(len(data)))

	// Data
	copy(w[24:], data)

	return out
}

func buildOpenAndXResponse(req []byte, fid uint16, info fs.FileInfo, fileAttrs uint16) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+(30)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 15 // WCT
	w := out[smbHeaderLen+1:]

	attrs := uint16(0)
	if info.IsDir() {
		attrs |= FileAttributeDirectory
	} else {
		attrs |= FileAttributeArchive
	}

	// AndXCommand, AndXReserved, AndXOffset
	w[0] = 0xFF
	w[1] = 0x00
	binary.LittleEndian.PutUint16(w[2:4], 0)

	// FID
	binary.LittleEndian.PutUint16(w[4:6], fid)

	// FileAttributes
	binary.LittleEndian.PutUint16(w[6:8], attrs)

	// LastWriteTime (DOS format, for now 0)
	binary.LittleEndian.PutUint32(w[8:12], 0)

	// FileSize
	binary.LittleEndian.PutUint32(w[12:16], uint32(info.Size()))

	// GrantedAccess
	binary.LittleEndian.PutUint16(w[16:18], 0x0001) // Read access

	// FileType
	binary.LittleEndian.PutUint16(w[18:20], 0) // DISK_FILE

	// DeviceState
	binary.LittleEndian.PutUint16(w[20:22], 0)

	// ActionOpened
	binary.LittleEndian.PutUint16(w[22:24], 0x0001) // FILE_OPENED

	// Reserved
	binary.LittleEndian.PutUint32(w[24:28], 0)

	// Reserved
	binary.LittleEndian.PutUint16(w[28:30], 0)

	// ByteCount = 0
	binary.LittleEndian.PutUint16(w[30:32], 0)

	return out
}

type searchResultEntry struct {
	name       string
	size       int64
	modTime    time.Time
	isDir      bool
	attributes uint16
}

func matchesSearchAttrs(info fs.FileInfo, searchAttrs uint16) bool {
	// searchAttrs indicates which types of files to include
	// If bit not set, the file type should not be included
	if info.IsDir() {
		return (searchAttrs & SearchAttributeDirectory) != 0
	}

	// Regular files always match unless only searching for specific types
	// that don't apply to normal files
	return true
}

func matchesPattern(name string, pattern string) bool {
	if pattern == "*" || pattern == "*.*" {
		return true
	}
	// Simple pattern matching: just check for exact match or wildcard suffix
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
	}
	return strings.ToLower(name) == strings.ToLower(pattern)
}

func getSearchAttrs(info fs.FileInfo) uint16 {
	var attrs uint16
	if info.IsDir() {
		attrs |= SearchAttributeDirectory
	}
	// TODO: check file mode/permissions for read-only, hidden, system, archive
	attrs |= SearchAttributeArchive
	return attrs
}

func buildSearchEmptyResponse(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	out := make([]byte, smbHeaderLen+1+2+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], 0) // EntryCount = 0
	binary.LittleEndian.PutUint16(w[2:4], 0) // ByteCount = 0
	return out
}

func buildSearchResponse(req []byte, entries []searchResultEntry) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	if len(entries) == 0 {
		return buildSearchEmptyResponse(req)
	}

	// Calculate data size
	var dataBuf bytes.Buffer
	for _, entry := range entries {
		// ResumeKey (21 bytes)
		dataBuf.Write(make([]byte, 21)) // Placeholder resume key

		// Attributes (2 bytes)
		var attrsBytes [2]byte
		binary.LittleEndian.PutUint16(attrsBytes[:], entry.attributes)
		dataBuf.Write(attrsBytes[:])

		// LastWriteTime (4 bytes, DOS format)
		timeVal := uint32(0) // TODO: convert time.Time to DOS format
		var timeBytes [4]byte
		binary.LittleEndian.PutUint32(timeBytes[:], timeVal)
		dataBuf.Write(timeBytes[:])

		// FileSize (4 bytes)
		var sizeBytes [4]byte
		binary.LittleEndian.PutUint32(sizeBytes[:], uint32(entry.size))
		dataBuf.Write(sizeBytes[:])

		// Filename (NUL-terminated)
		dataBuf.WriteString(entry.name)
		dataBuf.WriteByte(0)
	}

	dataBytes := dataBuf.Bytes()
	out := make([]byte, smbHeaderLen+1+2+2+len(dataBytes))
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 1 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(len(entries)))
	binary.LittleEndian.PutUint16(w[2:4], uint16(len(dataBytes)))
	copy(w[4:], dataBytes)
	return out
}

func parseTreeConnectShareName(req []byte) (string, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", false
	}

	for _, part := range splitNULStrings(bytesArea) {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if strings.Contains(p, "\\") {
			trimmed := strings.TrimLeft(p, "\\")
			segments := strings.Split(trimmed, "\\")
			if len(segments) >= 2 && segments[1] != "" {
				return segments[1], true
			}
		}
	}
	return "", false
}

// parseSMBPath extracts a path from the bytes area of an SMB request.
func parseSMBPath(req []byte) (string, bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", false
	}

	// Skip the path format indicator (typically buffer format code 0x04)
	rest := bytesArea
	if len(rest) > 0 && rest[0] == 0x04 {
		rest = rest[1:]
	}

	// Find the first NUL-terminated string
	if nulIdx := bytes.IndexByte(rest, 0); nulIdx >= 0 {
		pathStr := string(rest[:nulIdx])
		path := strings.TrimSpace(pathStr)
		// Strip leading separators and normalize
		path = strings.TrimLeft(path, "\\")
		return path, path != ""
	}

	return "", false
}

func smbBytesArea(req []byte) ([]byte, bool) {
	if len(req) < smbHeaderLen+3 {
		return nil, false
	}
	wct := int(req[smbHeaderLen])
	bytesOffset := smbHeaderLen + 1 + (wct * 2)
	if bytesOffset+2 > len(req) {
		return nil, false
	}
	byteCount := int(binary.LittleEndian.Uint16(req[bytesOffset : bytesOffset+2]))
	if byteCount < 0 || bytesOffset+2+byteCount > len(req) {
		return nil, false
	}
	return req[bytesOffset+2 : bytesOffset+2+byteCount], true
}

func splitNULStrings(b []byte) []string {
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] != 0 {
			continue
		}
		if i > start {
			parts = append(parts, string(b[start:i]))
		}
		start = i + 1
	}
	if start < len(b) {
		parts = append(parts, string(b[start:]))
	}
	return parts
}

// buildSimpleSuccessResponse returns an SMB response with success status,
// WCT=0 and ByteCount=0. Suitable for simple acknowledgement commands
// like Tree Disconnect where no payload is required.
func buildSimpleSuccessResponse(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+3)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 0
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1:smbHeaderLen+3], 0)
	return out
}

// buildQueryInformationDiskResponse constructs an SMB_COM_QUERY_INFORMATION_DISK
// response. Uses 512-byte blocks with 8-block allocation units (4KB clusters).
func buildQueryInformationDiskResponse(req []byte, totalBytes, freeBytes uint64) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}

	const blockSize = 512
	const blocksPerUnit = 8
	const allocationUnitSize = blockSize * blocksPerUnit

	totalUnits := uint16(totalBytes / allocationUnitSize)
	freeUnits := uint16(freeBytes / allocationUnitSize)

	out := make([]byte, smbHeaderLen+1+(5*2)+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80
	out[smbHeaderLen] = 5 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], totalUnits)
	binary.LittleEndian.PutUint16(w[2:4], blocksPerUnit)
	binary.LittleEndian.PutUint16(w[4:6], blockSize)
	binary.LittleEndian.PutUint16(w[6:8], freeUnits)
	binary.LittleEndian.PutUint16(w[8:10], 0) // Reserved
	binary.LittleEndian.PutUint16(w[10:12], 0) // ByteCount
	return out
}

// parseLANMANFunctionCode reads the RAP function code from the bytes
// area of a Transaction request targeting \PIPE\LANMAN. Returns false
// when the payload is too short to contain a function code.
func parseLANMANFunctionCode(req []byte) (uint16, bool) {
	bytesArea, ok := transactionBytesArea(req)
	if !ok {
		return 0, false
	}
	pipe := []byte("\\PIPE\\LANMAN\x00")
	idx := bytes.Index(bytes.ToUpper(bytesArea), bytes.ToUpper(pipe))
	if idx < 0 {
		return 0, false
	}
	p := idx + len(pipe)
	if p+2 > len(bytesArea) {
		return 0, false
	}
	return binary.LittleEndian.Uint16(bytesArea[p : p+2]), true
}

// parseNetServerEnum2ServerType best-effort parses the server-type
// filter (SV_TYPE_*) from a RAP NetServerEnum2 request.
func parseNetServerEnum2ServerType(req []byte) (uint32, bool) {
	bytesArea, ok := transactionBytesArea(req)
	if !ok {
		return 0, false
	}
	pipe := []byte("\\PIPE\\LANMAN\x00")
	idx := bytes.Index(bytes.ToUpper(bytesArea), bytes.ToUpper(pipe))
	if idx < 0 {
		return 0, false
	}
	p := idx + len(pipe)
	if p+2 > len(bytesArea) || binary.LittleEndian.Uint16(bytesArea[p:p+2]) != rapNetServerEnum2 {
		return 0, false
	}
	p += 2
	// Skip ParamDesc and DataDesc (both NUL-terminated strings).
	for i := 0; i < 2; i++ {
		n := bytes.IndexByte(bytesArea[p:], 0)
		if n < 0 {
			return 0, false
		}
		p += n + 1
		if p > len(bytesArea) {
			return 0, false
		}
	}
	if p+2+4 > len(bytesArea) {
		return 0, false
	}
	p += 2 // ReceiveBufferLength
	return binary.LittleEndian.Uint32(bytesArea[p : p+4]), true
}

// parseNetServerEnum2Domain extracts the optional Domain filter string from a RAP
// NetServerEnum2 request. Returns ("", false) when the field is absent.
func parseNetServerEnum2Domain(req []byte) (string, bool) {
	bytesArea, ok := transactionBytesArea(req)
	if !ok {
		return "", false
	}
	pipe := []byte("\\PIPE\\LANMAN\x00")
	idx := bytes.Index(bytes.ToUpper(bytesArea), bytes.ToUpper(pipe))
	if idx < 0 {
		return "", false
	}
	p := idx + len(pipe)
	if p+2 > len(bytesArea) || binary.LittleEndian.Uint16(bytesArea[p:p+2]) != rapNetServerEnum2 {
		return "", false
	}
	p += 2
	// Skip ParamDesc and DataDesc (both NUL-terminated).
	for i := 0; i < 2; i++ {
		n := bytes.IndexByte(bytesArea[p:], 0)
		if n < 0 {
			return "", false
		}
		p += n + 1
	}
	if p+2+4 > len(bytesArea) {
		return "", false
	}
	p += 2 + 4 // ReceiveBufferLength + ServerType
	if p >= len(bytesArea) {
		return "", false
	}
	n := bytes.IndexByte(bytesArea[p:], 0)
	if n < 0 {
		return "", false
	}
	domain := string(bytesArea[p : p+n])
	if domain == "" {
		return "", false
	}
	return domain, true
}

// smbServerList returns the server entries for a NetServerEnum2 response:
// ClassicStack itself plus any servers observed via browser announcements.
func (s *Service) smbServerList() []netServerInfo1 {
	self := normalizeBrowserName(s.opts.ServerName)
	if self == "" {
		self = "CLASSICSTACK"
	}
	entries := []netServerInfo1{{
		Name: self,
		Type: browserServerTypeWorkstationMask,
	}}
	s.mu.Lock()
	for name, rec := range s.browserServers {
		if name == self {
			continue
		}
		entries = append(entries, netServerInfo1{Name: name, Type: rec.ServerType})
	}
	s.mu.Unlock()
	return entries
}

// netServerEnum2Entries returns the entries and a RAP status code (0 = success).
//
// Per MS-BRWS §3.3.5.6:
//   - Potential browsers MUST return ERROR_REQ_NOT_ACCEP (71).
//   - SV_TYPE_DOMAIN_ENUM with any other type bit MUST return ERROR_INVALID_FUNCTION (1).
//   - SV_TYPE_DOMAIN_ENUM alone → return all observed machine groups.
func (s *Service) netServerEnum2Entries(serverType uint32, workgroup, requestedDomain string) ([]netServerInfo1, uint16) {
	s.mu.Lock()
	role := s.browserRole
	s.mu.Unlock()

	if role == browserRolePotential {
		return nil, rapStatusErrReqNotAccepted
	}

	if serverType&browserServerTypeDomainEnumMask != 0 {
		if serverType != browserServerTypeDomainEnumMask {
			// DOMAIN_ENUM mixed with other type bits is invalid.
			return nil, rapStatusErrInvalidFunction
		}
		// Return our own workgroup plus any domains observed via DomainAnnouncement.
		ownDomain := normalizeBrowserName(workgroup)
		if ownDomain == "" {
			ownDomain = "WORKGROUP"
		}
		groups := []netServerInfo1{{Name: ownDomain, Type: browserServerTypeDomainEnumMask}}
		s.mu.Lock()
		for group, rec := range s.machineGroups {
			if group == ownDomain {
				continue
			}
			groups = append(groups, netServerInfo1{
				Name:    group,
				Type:    browserServerTypeDomainEnumMask,
				Comment: rec.MasterBrowser,
			})
		}
		s.mu.Unlock()
		return groups, 0
	}

	// Server-list request: if the client specifies a domain, only return
	// results when it matches our own workgroup.
	if requestedDomain != "" {
		ownDomain := normalizeBrowserName(workgroup)
		if ownDomain == "" {
			ownDomain = "WORKGROUP"
		}
		if !strings.EqualFold(requestedDomain, ownDomain) {
			return nil, 0 // empty success — we don't serve that domain
		}
	}

	return s.smbServerList(), 0
}

// buildNetServerEnum2RAPErrorResponse wraps a non-zero RAP status code in a
// minimal SMB_COM_TRANSACTION success frame (SMB status is SUCCESS; the error
// is conveyed in the 2-byte RAP Status field of the parameter block).
func buildNetServerEnum2RAPErrorResponse(req []byte, rapStatus uint16) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	const paramLen = 8                       // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	paramOffset := smbHeaderLen + 1 + 20 + 2 // = 55
	totalLen := paramOffset + paramLen

	out := make([]byte, totalLen)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80

	out[smbHeaderLen] = 10 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(paramLen)) // TotalParameterCount
	binary.LittleEndian.PutUint16(w[6:8], uint16(paramLen)) // ParameterCount
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen)) // ByteCount

	p := out[paramOffset:]
	binary.LittleEndian.PutUint16(p[0:2], rapStatus)
	return out
}

// buildNetServerEnum2Response constructs an SMB_COM_TRANSACTION response
// carrying a RAP NetServerEnum2 reply with the supplied server entries.
// Converter is set to zero; CommentOffset fields are offsets from the
// start of the Transaction data block.
func buildNetServerEnum2Response(req []byte, entries []netServerInfo1) []byte {
	if len(req) < smbHeaderLen {
		return nil
	}
	const entrySize = 26 // SERVER_INFO_1: Name(16)+VMaj(1)+VMin(1)+Type(4)+CommentOff(4)

	commentBase := len(entries) * entrySize

	commentOff := commentBase
	commentData := make([]byte, 0, len(entries))
	commentOffsets := make([]int, len(entries))
	for i, e := range entries {
		commentOffsets[i] = commentOff
		commentData = append(commentData, []byte(e.Comment)...)
		commentData = append(commentData, 0)
		commentOff += len(e.Comment) + 1
	}

	paramLen := 8 // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	dataLen := len(entries)*entrySize + len(commentData)

	// Layout: header(32) + WCT(1) + 10 words(20) + ByteCount(2) + params + data.
	paramOffset := smbHeaderLen + 1 + 20 + 2 // = 55
	dataOffset := paramOffset + paramLen     // = 63
	totalLen := dataOffset + dataLen

	out := make([]byte, totalLen)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[smbOffStatus:smbOffStatus+4], smbStatusSuccess)
	out[smbOffFlags] |= 0x80

	out[smbHeaderLen] = 10 // WCT
	w := out[smbHeaderLen+1:]
	binary.LittleEndian.PutUint16(w[0:2], uint16(paramLen))           // TotalParameterCount
	binary.LittleEndian.PutUint16(w[2:4], uint16(dataLen))            // TotalDataCount
	binary.LittleEndian.PutUint16(w[6:8], uint16(paramLen))           // ParameterCount
	binary.LittleEndian.PutUint16(w[8:10], uint16(paramOffset))       // ParameterOffset
	binary.LittleEndian.PutUint16(w[12:14], uint16(dataLen))          // DataCount
	binary.LittleEndian.PutUint16(w[14:16], uint16(dataOffset))       // DataOffset
	binary.LittleEndian.PutUint16(w[20:22], uint16(paramLen+dataLen)) // ByteCount

	p := out[paramOffset:]
	// p[0:2] Status = 0, p[2:4] Converter = 0 (already zero from make).
	binary.LittleEndian.PutUint16(p[4:6], uint16(len(entries))) // EntriesReturned
	binary.LittleEndian.PutUint16(p[6:8], uint16(len(entries))) // EntriesAvailable

	d := out[dataOffset:]
	for i, e := range entries {
		base := i * entrySize
		name := normalizeBrowserName(e.Name)
		if len(name) > 15 {
			name = name[:15]
		}
		copy(d[base:base+16], []byte(name)) // remaining bytes stay NUL
		d[base+16] = 4                      // sv1_version_major
		// d[base+17] = 0 sv1_version_minor (already zero)
		binary.LittleEndian.PutUint32(d[base+18:base+22], e.Type)
		binary.LittleEndian.PutUint32(d[base+22:base+26], uint32(commentOffsets[i]))
	}
	copy(d[commentBase:], commentData)

	return out
}

func buildSMBTransactionEmptySuccess(req []byte) []byte {
	if len(req) < smbHeaderLen || string(req[0:4]) != "\xffSMB" {
		return nil
	}
	out := make([]byte, smbHeaderLen+1+20+2)
	copy(out[:smbHeaderLen], req[:smbHeaderLen])
	binary.LittleEndian.PutUint32(out[5:9], smbStatusSuccess)
	out[9] = req[9] | 0x80
	out[32] = 10 // TRANSACTION response word count
	// 20-byte parameter block left as zero (no params/data)
	binary.LittleEndian.PutUint16(out[smbHeaderLen+1+20:smbHeaderLen+1+22], 0)
	return out
}

// HandleDatagram implements netbios.CommandHandler.
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
