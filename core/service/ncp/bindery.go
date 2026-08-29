package ncp

// bindery.go implements the bindery object read family — Get Bindery Object ID
// (0x17/0x35), Get Bindery Object Name (0x17/0x36) and Scan Bindery Object
// (0x17/0x37) — over a small static bindery: the well-known objects a NetWare 3.x
// server always carries (SUPERVISOR, GUEST, the EVERYONE group) plus the server's
// own file-server object. Clients resolve the login user object (typically GUEST)
// through these before issuing the login verb, so answering them 0xFB stalls the
// attach; a lookup miss must be the bindery "no such object" completion instead.
//
// Reference: mars_nwe nwbind.c cases 0x35/0x36/0x37 (find_obj_id / nw_get_obj /
// scan_for_obj) and nwdbm.c nw_fill_standard for the well-known objects.

import "strings"

// Bindery object types (Novell bindery OT_* values).
const (
	objTypeUser       uint16 = 0x0001 // OT_USER
	objTypeUserGroup  uint16 = 0x0002 // OT_USER_GROUP
	objTypeFileServer uint16 = 0x0004 // OT_FILE_SERVER
	objTypeWildcard   uint16 = 0xFFFF // wildcard type (Scan Bindery Object only)
)

// Well-known bindery object ids, following mars_nwe nwdbm.c nw_fill_standard
// (su_id 0x00000001, ge_id 0x01000001, server_id 0x03000001); GUEST takes the
// unused 0x02000001 slot in the same pattern.
const (
	objIDSupervisor uint32 = 0x00000001
	objIDEveryone   uint32 = 0x01000001
	objIDGuest      uint32 = 0x02000001
	objIDServer     uint32 = 0x03000001
)

// binderyObject is one bindery object (mars_nwe NETOBJ): id, type, the up-to-47
// character name, the object flag (0 = static) and the security byte (0x31 =
// anyone may read, supervisor may write — the mars_nwe default for the standard
// objects).
type binderyObject struct {
	id       uint32
	typ      uint16
	name     string
	flags    uint8
	security uint8
}

// binderyObjects returns the static bindery in scan order (ascending id). The
// server object carries the live configured server name.
func (s *Service) binderyObjects() []binderyObject {
	return []binderyObject{
		{id: objIDSupervisor, typ: objTypeUser, name: "SUPERVISOR", security: 0x31},
		{id: objIDGuest, typ: objTypeUser, name: "GUEST", security: 0x31},
		{id: objIDEveryone, typ: objTypeUserGroup, name: "EVERYONE", security: 0x31},
		{id: objIDServer, typ: objTypeFileServer, name: s.serverName(), security: 0x31},
	}
}

// loginObjectFor resolves a login name to its bindery user object (id + type).
// An empty or unknown name maps to GUEST — the guest-equivalent grant binds the
// connection to the GUEST identity, so the connection-information family
// reports a real bindery object for it.
func (s *Service) loginObjectFor(user string) (uint32, uint16) {
	for _, o := range s.binderyObjects() {
		if o.typ == objTypeUser && strings.EqualFold(o.name, user) {
			return o.id, o.typ
		}
	}
	return objIDGuest, objTypeUser
}

// getBinderyObjectID answers Get Bindery Object ID (0x17/0x35): args = object
// type (2 BE) then the length-prefixed object name (no wildcards); reply =
// object id (4 BE) + object type (2 BE) + object name[48]. A miss is the bindery
// no-such-object completion. Per mars_nwe nwbind.c case 0x35 (find_obj_id).
func (cn *Conn) getBinderyObjectID(args []byte) ([]byte, error) {
	if len(args) < 3 {
		return nil, errNoSuchObject
	}
	typ := uint16(args[0])<<8 | uint16(args[1])
	name, _, ok := readByteString(args, 2)
	if !ok {
		return nil, errNoSuchObject
	}
	for _, o := range cn.svc.binderyObjects() {
		if o.typ == typ && strings.EqualFold(o.name, name) {
			return appendObjectReply(nil, o), nil
		}
	}
	return nil, errNoSuchObject
}

// getBinderyObjectName answers Get Bindery Object Name (0x17/0x36): args =
// object id (4 BE); reply = the same id+type+name[48] shape as get-ID. Per
// mars_nwe nwbind.c case 0x36 (nw_get_obj).
func (cn *Conn) getBinderyObjectName(args []byte) ([]byte, error) {
	if len(args) < 4 {
		return nil, errNoSuchObject
	}
	id := uint32(args[0])<<24 | uint32(args[1])<<16 | uint32(args[2])<<8 | uint32(args[3])
	for _, o := range cn.svc.binderyObjects() {
		if o.id == id {
			return appendObjectReply(nil, o), nil
		}
	}
	return nil, errNoSuchObject
}

// scanBinderyObject answers Scan Bindery Object (0x17/0x37): args = last object
// id (4 BE; 0xFFFFFFFF starts the scan), object type (2 BE; 0xFFFF = any), then
// the length-prefixed name pattern ('*'/'?' wildcards). The reply extends the
// id+type+name[48] shape with the object flag, the security byte, and a
// has-properties flag (0 — this bindery carries no properties). The scan returns
// the first match AFTER the last id in bindery order; no further match ends the
// scan with the no-such-object completion. Per mars_nwe nwbind.c case 0x37
// (scan_for_obj).
func (cn *Conn) scanBinderyObject(args []byte) ([]byte, error) {
	if len(args) < 7 {
		return nil, errNoSuchObject
	}
	lastID := uint32(args[0])<<24 | uint32(args[1])<<16 | uint32(args[2])<<8 | uint32(args[3])
	typ := uint16(args[4])<<8 | uint16(args[5])
	pattern, _, ok := readByteString(args, 6)
	if !ok {
		return nil, errNoSuchObject
	}
	objs := cn.svc.binderyObjects()
	start := 0
	if lastID != 0xFFFFFFFF {
		for i, o := range objs {
			if o.id == lastID {
				start = i + 1
				break
			}
		}
	}
	for _, o := range objs[start:] {
		if typ != objTypeWildcard && o.typ != typ {
			continue
		}
		if !matchBinderyName(pattern, o.name) {
			continue
		}
		out := appendObjectReply(nil, o)
		out = append(out, o.flags, o.security, 0 /* has-properties */)
		return out, nil
	}
	return nil, errNoSuchObject
}

// appendObjectReply appends the common bindery-object reply fields: object id
// (4 BE), object type (2 BE), and the NUL-padded 48-byte object name.
func appendObjectReply(dst []byte, o binderyObject) []byte {
	dst = appendU32(dst, o.id)
	dst = appendU16(dst, o.typ)
	var name [48]byte
	copy(name[:], o.name)
	return append(dst, name[:]...)
}

// matchBinderyName matches a bindery name against a scan pattern, case-
// insensitively: '*' matches any run (including empty), '?' matches one
// character (mars_nwe nwdbm.c name_match).
func matchBinderyName(pattern, name string) bool {
	p := strings.ToUpper(pattern)
	n := strings.ToUpper(name)
	return wildcardMatch(p, n)
}

// wildcardMatch is a plain iterative '*'/'?' glob matcher.
func wildcardMatch(p, s string) bool {
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == s[si]):
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			mark++
			pi, si = star+1, mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
