package browser

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// frames.go holds the self-serialising browser frame DTOs (the DTO rule): each
// type Marshals to / Unmarshals from its [MS-BRWS] wire form. These are the bare
// browser frames — the SMB_COM_TRANSACTION mailslot envelope that carries them is
// core/protocol/mailslot (§3-quater), wrapped/unwrapped by the mailslot dispatch
// layer, never by the browser.

// --- HostAnnouncement (0x01) / LocalMasterAnnouncement (0x0F) ---

// Announcement is a host (0x01) or local-master (0x0F) announcement frame. The two
// share a 33-byte fixed layout + optional comment; Op selects which opcode is
// emitted/expected.
type Announcement struct {
	Op             uint8 // OpHostAnnouncement or OpLocalMasterAnnounce
	UpdateCount    uint8
	PeriodicityMS  uint32
	ServerName     string
	OSVersionMajor uint8
	OSVersionMinor uint8
	ServerType     uint32
	VersionMajor   uint8
	VersionMinor   uint8
	Comment        string
}

// Marshal renders an announcement frame (33-byte fixed header + optional
// NUL-terminated comment).
func (f Announcement) Marshal() []byte {
	out := make([]byte, 33)
	out[0] = f.Op
	out[1] = f.UpdateCount
	bp.PutLE32(out[2:6], f.PeriodicityMS)
	name := fixedName(f.ServerName)
	copy(out[6:22], name[:])
	out[22] = f.OSVersionMajor
	out[23] = f.OSVersionMinor
	bp.PutLE32(out[24:28], f.ServerType)
	out[28] = f.VersionMajor
	out[29] = f.VersionMinor
	bp.PutLE16(out[30:32], Signature)
	comment := f.Comment
	if len(comment) > 42 {
		comment = comment[:42]
	}
	if comment != "" {
		return append(out[:32], append([]byte(comment), 0)...)
	}
	return out
}

// UnmarshalAnnouncement parses a host or local-master announcement.
func UnmarshalAnnouncement(b []byte) (*Announcement, error) {
	if len(b) < 33 {
		return nil, ErrShort
	}
	if b[0] != OpHostAnnouncement && b[0] != OpLocalMasterAnnounce {
		return nil, ErrBadOp
	}
	comment := ""
	if len(b) > 32 {
		comment = parseName(b[32:])
	}
	return &Announcement{
		Op:             b[0],
		UpdateCount:    b[1],
		PeriodicityMS:  bp.LE32(b[2:6]),
		ServerName:     parseName(b[6:22]),
		OSVersionMajor: b[22],
		OSVersionMinor: b[23],
		ServerType:     bp.LE32(b[24:28]),
		VersionMajor:   b[28],
		VersionMinor:   b[29],
		Comment:        comment,
	}, nil
}

// --- DomainAnnouncement (0x0C) ---

// DomainAnnouncement is a workgroup/domain announcement (0x0C): the machine group
// and the local master browser that owns it.
type DomainAnnouncement struct {
	PeriodicityMS uint32
	MachineGroup  string
	ServerType    uint32
	LocalMaster   string
}

// UnmarshalDomainAnnouncement parses a domain announcement ([MS-BRWS] §2.2.7).
func UnmarshalDomainAnnouncement(b []byte) (*DomainAnnouncement, error) {
	const fixed = 32
	if len(b) < fixed+1 {
		return nil, ErrShort
	}
	if b[0] != OpDomainAnnouncement {
		return nil, ErrBadOp
	}
	return &DomainAnnouncement{
		PeriodicityMS: bp.LE32(b[2:6]),
		MachineGroup:  parseName(b[6:22]),
		ServerType:    bp.LE32(b[24:28]),
		LocalMaster:   parseName(b[fixed:]),
	}, nil
}

// --- RequestElection (0x08) ---

// Election is a master-browser election frame (0x08): the candidate's criteria,
// uptime, and name, compared by Compare to decide the winner.
type Election struct {
	Version    uint8
	Criteria   uint32
	Uptime     uint32
	Reserved   uint32
	ServerName string
}

// Marshal renders an election frame (14-byte fixed + NUL-terminated name).
func (f Election) Marshal() []byte {
	out := make([]byte, 14)
	out[0] = OpRequestElection
	out[1] = f.Version
	bp.PutLE32(out[2:6], f.Criteria)
	bp.PutLE32(out[6:10], f.Uptime)
	bp.PutLE32(out[10:14], f.Reserved)
	return appendName(out, f.ServerName)
}

// UnmarshalElection parses a request-election frame.
func UnmarshalElection(b []byte) (*Election, error) {
	if len(b) < 15 {
		return nil, ErrShort
	}
	if b[0] != OpRequestElection {
		return nil, ErrBadOp
	}
	return &Election{
		Version:    b[1],
		Criteria:   bp.LE32(b[2:6]),
		Uptime:     bp.LE32(b[6:10]),
		Reserved:   bp.LE32(b[10:14]),
		ServerName: parseName(b[14:]),
	}, nil
}

// Compare returns >0 if local wins the election over remote, <0 if it loses, 0 on
// a tie ([MS-BRWS] §3.3: higher criteria wins, then higher uptime, then
// lexicographically LOWER name). A tie usually means our own broadcast echoed back.
func Compare(local, remote Election) int {
	switch {
	case local.Criteria != remote.Criteria:
		return cmpU32(local.Criteria, remote.Criteria)
	case local.Uptime != remote.Uptime:
		return cmpU32(local.Uptime, remote.Uptime)
	}
	// Lower name wins → invert the lexical comparison.
	switch cmp := strCompare(NormalizeName(local.ServerName), NormalizeName(remote.ServerName)); {
	case cmp < 0:
		return 1
	case cmp > 0:
		return -1
	default:
		return 0
	}
}

func cmpU32(a, b uint32) int {
	if a > b {
		return 1
	}
	return -1
}

func strCompare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// --- GetBackupList request (0x09) / response (0x0A) ---

// GetBackupListRequest is the GetBackupList request (0x09): how many backup
// servers the caller wants and a token echoed in the response.
type GetBackupListRequest struct {
	RequestedCount uint8
	Token          uint32
}

// Marshal renders the request (6 bytes).
func (f GetBackupListRequest) Marshal() []byte {
	out := make([]byte, 6)
	out[0] = OpGetBackupListReq
	out[1] = f.RequestedCount
	bp.PutLE32(out[2:6], f.Token)
	return out
}

// UnmarshalGetBackupListRequest parses a GetBackupList request.
func UnmarshalGetBackupListRequest(b []byte) (*GetBackupListRequest, error) {
	if len(b) < 6 {
		return nil, ErrShort
	}
	if b[0] != OpGetBackupListReq {
		return nil, ErrBadOp
	}
	return &GetBackupListRequest{RequestedCount: b[1], Token: bp.LE32(b[2:6])}, nil
}

// GetBackupListResponse is the GetBackupList response (0x0A): the echoed token and
// the list of backup browser server names.
type GetBackupListResponse struct {
	Token         uint32
	BackupServers []string
}

// Marshal renders the response (6-byte header + NUL-terminated server names).
func (f GetBackupListResponse) Marshal() []byte {
	out := make([]byte, 6)
	out[0] = OpGetBackupListResp
	out[1] = uint8(len(f.BackupServers))
	bp.PutLE32(out[2:6], f.Token)
	for _, s := range f.BackupServers {
		out = appendName(out, s)
	}
	return out
}

// UnmarshalGetBackupListResponse parses a GetBackupList response.
func UnmarshalGetBackupListResponse(b []byte) (*GetBackupListResponse, error) {
	if len(b) < 6 {
		return nil, ErrShort
	}
	if b[0] != OpGetBackupListResp {
		return nil, ErrBadOp
	}
	count := int(b[1])
	servers := make([]string, 0, count)
	rest := b[6:]
	for len(rest) > 0 && len(servers) < count {
		i := indexByte(rest, 0)
		if i < 0 {
			return nil, ErrShort
		}
		servers = append(servers, parseName(rest[:i]))
		rest = rest[i+1:]
	}
	return &GetBackupListResponse{Token: bp.LE32(b[2:6]), BackupServers: servers}, nil
}

// --- AnnouncementRequest (0x02) ---

// AnnouncementRequest is a request that listening servers re-announce themselves
// (0x02), optionally naming where to respond.
type AnnouncementRequest struct {
	Reserved     uint8
	ResponseName string
}

// Marshal renders an announcement request ([MS-BRWS] §2.2.2): the opcode, a reserved
// byte, then an optional NUL-terminated response computer name (the browser a
// re-announcing host should unicast its HostAnnouncement to; empty asks for the usual
// broadcast). A browse client emits this to solicit an immediate re-announce from every
// listening browser rather than waiting for the periodic timer.
func (f AnnouncementRequest) Marshal() []byte {
	out := []byte{OpAnnouncementRequest, f.Reserved}
	if f.ResponseName != "" {
		out = appendName(out, f.ResponseName)
	}
	return out
}

// UnmarshalAnnouncementRequest parses an announcement request.
func UnmarshalAnnouncementRequest(b []byte) (*AnnouncementRequest, error) {
	if len(b) < 2 {
		return nil, ErrShort
	}
	if b[0] != OpAnnouncementRequest {
		return nil, ErrBadOp
	}
	name := ""
	if len(b) > 2 {
		name = parseName(b[2:])
	}
	return &AnnouncementRequest{Reserved: b[1], ResponseName: name}, nil
}
