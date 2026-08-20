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
// share a 32-byte fixed layout + a NUL-terminated comment; Op selects which opcode is
// emitted/expected. Golden bytes, spec/captures/nbf-win98.pcap frame 61 (a Win98
// local-master announcement):
//
//	0f 04 c0 d4 01 00 "WIN98-NBF-1"+NUL-pad(16) 04 00 03 20 44 00 15 04 55 aa "86box win98 nbf" 00
//
// — UpdateCount 4, periodicity 120000, OS 4.0, ServerType 0x00442003, browser
// protocol 21.4, signature 0xAA55, then the comment.
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

// announcementFixed is the fixed-header length of a host/local-master announcement;
// the NUL-terminated comment follows it (an empty comment is the bare NUL, which is
// why the minimum frame is announcementFixed+1 = AnnouncementMinLen).
const announcementFixed = 32

// Marshal renders an announcement frame (32-byte fixed header + NUL-terminated
// comment).
func (f Announcement) Marshal() []byte {
	out := make([]byte, announcementFixed+1)
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
	if len(comment) > maxCommentLen {
		comment = comment[:maxCommentLen]
	}
	if comment != "" {
		return append(out[:announcementFixed], append([]byte(comment), 0)...)
	}
	return out
}

// maxCommentLen is the longest server comment an announcement carries ([MS-BRWS]
// §2.2.1 Comment: at most 43 bytes including its NUL).
const maxCommentLen = 42

// UnmarshalAnnouncement parses a host or local-master announcement.
func UnmarshalAnnouncement(b []byte) (*Announcement, error) {
	if len(b) < AnnouncementMinLen {
		return nil, ErrShort
	}
	if b[0] != OpHostAnnouncement && b[0] != OpLocalMasterAnnounce {
		return nil, ErrBadOp
	}
	comment := parseName(b[announcementFixed:])
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
// and the local master browser that owns it. A local master browser broadcasts one
// to __MSBROWSE__<01> alongside its periodic LocalMasterAnnouncement, so every other
// master on the segment learns the workgroup exists.
//
// The layout is the 32-byte announcement fixed header with the MachineGroup in the
// name field and the local master's name as the trailing NUL-terminated string
// (where a host announcement carries its comment). Golden bytes,
// spec/captures/nbf-win98.pcap frame 141:
//
//	0c 00 c0 d4 01 00 "WORKGROUP"+NUL-pad(16) 04 00 00 20 40 80 00 00 00 00 "WIN98-NBF-1" 00
//
// i.e. UpdateCount 0, periodicity 120000, OS 4.0, ServerType 0x80402000, and — unlike
// a host announcement — version bytes 0/0 and signature 0x0000, NOT 0xAA55.
type DomainAnnouncement struct {
	UpdateCount    uint8
	PeriodicityMS  uint32
	MachineGroup   string
	OSVersionMajor uint8
	OSVersionMinor uint8
	ServerType     uint32
	LocalMaster    string
}

// domainAnnouncementFixed is the fixed-header length shared with Announcement; the
// local-master name follows it.
const domainAnnouncementFixed = 32

// Marshal renders a domain announcement (32-byte fixed header + NUL-terminated local
// master name). The version bytes and signature stay zero, matching the golden frame.
func (f DomainAnnouncement) Marshal() []byte {
	out := make([]byte, domainAnnouncementFixed)
	out[0] = OpDomainAnnouncement
	out[1] = f.UpdateCount
	bp.PutLE32(out[2:6], f.PeriodicityMS)
	group := fixedName(f.MachineGroup)
	copy(out[6:22], group[:])
	out[22] = f.OSVersionMajor
	out[23] = f.OSVersionMinor
	bp.PutLE32(out[24:28], f.ServerType)
	return appendName(out, f.LocalMaster)
}

// UnmarshalDomainAnnouncement parses a domain announcement ([MS-BRWS] §2.2.7).
func UnmarshalDomainAnnouncement(b []byte) (*DomainAnnouncement, error) {
	if len(b) < DomainAnnouncementMinLen {
		return nil, ErrShort
	}
	if b[0] != OpDomainAnnouncement {
		return nil, ErrBadOp
	}
	return &DomainAnnouncement{
		UpdateCount:    b[1],
		PeriodicityMS:  bp.LE32(b[2:6]),
		MachineGroup:   parseName(b[6:22]),
		OSVersionMajor: b[22],
		OSVersionMinor: b[23],
		ServerType:     bp.LE32(b[24:28]),
		LocalMaster:    parseName(b[domainAnnouncementFixed:]),
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

// electionFixed is the fixed-header length of an election frame; the NUL-terminated
// candidate name follows it.
const electionFixed = 14

// Marshal renders an election frame (14-byte fixed + NUL-terminated name).
func (f Election) Marshal() []byte {
	out := make([]byte, electionFixed)
	out[0] = OpRequestElection
	out[1] = f.Version
	bp.PutLE32(out[2:6], f.Criteria)
	bp.PutLE32(out[6:10], f.Uptime)
	bp.PutLE32(out[10:14], f.Reserved)
	return appendName(out, f.ServerName)
}

// UnmarshalElection parses a request-election frame.
func UnmarshalElection(b []byte) (*Election, error) {
	if len(b) < ElectionMinLen {
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
		ServerName: parseName(b[electionFixed:]),
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

// Marshal renders the request. [MS-BRWS] §2.2.5 defines six bytes (opcode, requested
// count, 4-byte token) but every real Win98 GetBackupList request in the golden
// captures is SEVEN — a trailing NUL after the token (DataCount 7:
// spec/captures/nbf-win98.pcap frames 22/41/65, nbipx-win98.pcap frames 57/58,
// nwlink-win98.pcap frames 26–31). We emit the observed seven; Unmarshal accepts
// either, since the extra byte carries nothing.
func (f GetBackupListRequest) Marshal() []byte {
	out := make([]byte, getBackupListRequestLen)
	out[0] = OpGetBackupListReq
	out[1] = f.RequestedCount
	bp.PutLE32(out[2:6], f.Token)
	return out
}

// getBackupListRequestLen is the observed on-the-wire request length (see Marshal).
const getBackupListRequestLen = GetBackupListMinLen + 1

// UnmarshalGetBackupListRequest parses a GetBackupList request.
func UnmarshalGetBackupListRequest(b []byte) (*GetBackupListRequest, error) {
	if len(b) < GetBackupListMinLen {
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
	out := make([]byte, GetBackupListMinLen)
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
	if len(b) < GetBackupListMinLen {
		return nil, ErrShort
	}
	if b[0] != OpGetBackupListResp {
		return nil, ErrBadOp
	}
	count := int(b[1])
	servers := make([]string, 0, count)
	rest := b[GetBackupListMinLen:]
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
	if len(b) < AnnouncementRequestMinLen {
		return nil, ErrShort
	}
	if b[0] != OpAnnouncementRequest {
		return nil, ErrBadOp
	}
	name := ""
	if len(b) > AnnouncementRequestMinLen {
		name = parseName(b[AnnouncementRequestMinLen:])
	}
	return &AnnouncementRequest{Reserved: b[1], ResponseName: name}, nil
}
