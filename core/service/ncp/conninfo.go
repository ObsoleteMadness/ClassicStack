package ncp

// conninfo.go implements the connection-information family of the 0x17
// connection/bindery services: Get Connection Information (0x17/0x16 old,
// 0x17/0x1C new), Get Connection Internet Address (0x17/0x13 old, 0x17/0x1A
// new) and Get Object Connection List (0x17/0x15 old, 0x17/0x1B new). Clients
// use these right after attach to answer "who is logged in on connection N" —
// the Windows 9x NetWare client issues Get Connection Information about its own
// connection and treats a failure as "station not logged in", so answering
// 0xFB here blocks the login even though the login verb itself succeeded.
//
// Reference: mars_nwe nwbind.c cases 0x13/0x15/0x16/0x1a/0x1b/0x1c and
// get_login_time (CLAUDE.md #7).

import (
	"os"
	"sort"
	"strings"
	"time"
)

// connInfoReplyLen is the fixed Get Connection Information reply: object id
// (4 BE) + object type (2 BE) + object name[48] + login time[7] + reserved(1)
// (mars_nwe nwbind.c struct XDATA).
const connInfoReplyLen = 4 + 2 + 48 + 7 + 1

// targetConn reads the family's target-connection argument: the old forms carry
// a 1-byte connection number, the new (>255 connections) forms a 4-byte
// LITTLE-endian one (mars_nwe GET_32). ok=false on a truncated buffer.
func targetConn(args []byte, old bool) (uint32, bool) {
	if old {
		if len(args) < 1 {
			return 0, false
		}
		return uint32(args[0]), true
	}
	if len(args) < 4 {
		return 0, false
	}
	return uint32(args[0]) | uint32(args[1])<<8 | uint32(args[2])<<16 | uint32(args[3])<<24, true
}

// getConnectionInfo answers Get Connection Information (0x17/0x16 old,
// 0x17/0x1C new): who is logged in on the target connection. A number out of
// range is the bad-station completion; an in-range connection that is not live
// or not logged in answers success with an all-zero struct (mars_nwe nwbind.c
// case 0x16/0x1c).
func (cn *Conn) getConnectionInfo(args []byte, old bool) ([]byte, error) {
	num, ok := targetConn(args, old)
	if !ok || num == 0 || num > maxConnections {
		return nil, errBadStation
	}
	out := make([]byte, connInfoReplyLen)
	c, live := cn.svc.conns.Peek(uint16(num))
	if !live {
		return out, nil
	}
	c.mu.Lock()
	id := c.objectID
	at := c.loginTime
	c.mu.Unlock()
	if id == 0 {
		return out, nil
	}
	// Report the canonical bindery object for the logged-in id (mars_nwe
	// nw_get_obj), not the raw login string — an empty/unknown login name was
	// bound to GUEST at login time.
	for _, o := range cn.svc.binderyObjects() {
		if o.id != id {
			continue
		}
		out[0], out[1], out[2], out[3] = byte(o.id>>24), byte(o.id>>16), byte(o.id>>8), byte(o.id)
		out[4], out[5] = byte(o.typ>>8), byte(o.typ)
		copy(out[6:6+48], strings.ToUpper(o.name))
		putLoginTime(out[54:61], at)
		break
	}
	return out, nil
}

// putLoginTime encodes the 7-byte login-time field: year (since 1900), month
// (1-12), day, hour, minute, second, weekday (0 = Sunday) — mars_nwe
// get_login_time (struct tm fields verbatim).
func putLoginTime(dst []byte, at time.Time) {
	dst[0] = byte(at.Year() - 1900)
	dst[1] = byte(at.Month())
	dst[2] = byte(at.Day())
	dst[3] = byte(at.Hour())
	dst[4] = byte(at.Minute())
	dst[5] = byte(at.Second())
	dst[6] = byte(at.Weekday())
}

// getConnInternetAddress answers Get Connection Internet Address (0x17/0x13
// old, 0x17/0x1A new): the target connection's IPX address — network(4) +
// node(6) + socket(2); the new form appends the connection type byte 0x02
// (NCP). Any miss is the generic failure completion (mars_nwe nwbind.c case
// 0x13/0x1a answers 0xff).
func (cn *Conn) getConnInternetAddress(args []byte, old bool) ([]byte, error) {
	num, ok := targetConn(args, old)
	if !ok || num == 0 || num > maxConnections {
		return nil, os.ErrNotExist
	}
	c, live := cn.svc.conns.Peek(uint16(num))
	if !live {
		return nil, os.ErrNotExist
	}
	out := make([]byte, 0, 13)
	out = append(out, c.ep.net[:]...)
	out = append(out, c.ep.node[:]...)
	out = append(out, c.sock[:]...)
	if !old {
		out = append(out, 0x02) // connection type: NCP
	}
	return out, nil
}

// getObjectConnList answers Get Object Connection List (0x17/0x15 old,
// 0x17/0x1B new): every connection number the named bindery object is logged in
// on. Old form: args = object type (2 BE) + length-prefixed name; reply =
// count(1) + 1-byte connection numbers. New form: args are preceded by a 4-byte
// BE search offset (resume after that connection number) and the reply numbers
// are 2-byte LO-HI. A name miss is the no-such-object completion (mars_nwe
// nwbind.c cases 0x15/0x1b).
func (cn *Conn) getObjectConnList(args []byte, old bool) ([]byte, error) {
	var searchAfter uint32
	if !old {
		if len(args) < 4 {
			return nil, errNoSuchObject
		}
		searchAfter = uint32(args[0])<<24 | uint32(args[1])<<16 | uint32(args[2])<<8 | uint32(args[3])
		args = args[4:]
	}
	if len(args) < 3 {
		return nil, errNoSuchObject
	}
	typ := uint16(args[0])<<8 | uint16(args[1])
	name, _, ok := readByteString(args, 2)
	if !ok {
		return nil, errNoSuchObject
	}
	var obj *binderyObject
	for _, o := range cn.svc.binderyObjects() {
		if o.typ == typ && strings.EqualFold(o.name, name) {
			obj = &o
			break
		}
	}
	if obj == nil {
		return nil, errNoSuchObject
	}

	conns := cn.svc.conns.All()
	sort.Slice(conns, func(i, j int) bool { return conns[i].number < conns[j].number })
	out := []byte{0} // count, patched below
	count := 0
	for _, c := range conns {
		if count >= 255 {
			break
		}
		if uint32(c.number) < searchAfter {
			continue
		}
		c.mu.Lock()
		match := c.objectID == obj.id
		c.mu.Unlock()
		if !match {
			continue
		}
		if old {
			out = append(out, byte(c.number))
		} else {
			out = append(out, byte(c.number), byte(c.number>>8)) // LO-HI (mars_nwe U16_TO_16)
		}
		count++
	}
	out[0] = byte(count)
	return out, nil
}
