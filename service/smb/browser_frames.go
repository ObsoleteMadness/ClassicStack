package smb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"

	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
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
