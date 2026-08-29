// Package macresources is a Go port of Elliot Nunn's "macresources" library and its
// `rdump`/`derez` resource-fork text format.
//
//	Original work: macresources by Elliot Nunn
//	https://github.com/elliotnunn/macresources
//	Ported to Go and adapted to the ClassicStack storage seam (§9); all credit for the
//	format and the reference implementation is Elliot's.
//
// It converts between a binary classic-Mac RESOURCE FORK and a human-readable, Rez-like
// text representation (the "rdump" / DeRez form), so a resource fork can be checked into
// version control as text and round-tripped back to bytes. This is the codec behind the
// "derez" fork engine (core/fs/fork_derez.go), which a developer working on a classic
// codebase (e.g. a CodeWarrior project) can use to keep resources diffable in git.
//
// Two directions:
//   - ParseResourceFork(bin) → []Resource     (binary fork → records)
//   - BuildResourceFork(res) → bin            (records → binary fork)
//   - FormatRez(res) → text                   (records → rdump text)
//   - ParseRez(text) → []Resource             (rdump text → records)
//
// The binary layout follows the classic Resource Manager on-disk format (Inside
// Macintosh: More Macintosh Toolbox), exactly as the Python library reads/writes it.
package macresources

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Resource is one resource: a 4-byte type, a 16-bit signed ID, an optional name, the
// attribute byte, and the data bytes. Mirrors the Resource record in the Python library.
type Resource struct {
	Type    [4]byte
	ID      int16
	Name    string // empty when the resource has no name
	HasName bool
	Attribs byte
	Data    []byte
}

// Resource attribute bits (the named set the macresources tool recognises). changed
// (0x02) and compressed (0x01) are not surfaced as names, matching the reference.
const (
	AttrSysHeap   byte = 0x40
	AttrPurgeable byte = 0x20
	AttrLocked    byte = 0x10
	AttrProtected byte = 0x08
	AttrPreload   byte = 0x04
)

// ErrBadResourceFork is returned when the binary fork is structurally invalid.
var ErrBadResourceFork = errors.New("macresources: malformed resource fork")

// ParseResourceFork decodes a binary resource fork into its resources. An empty input
// yields no resources (a fork-less file). Follows the Python parse_file: a 16-byte
// header (data/map offsets+lengths), a resource map (type list + name list), per-type
// reference lists, and length-prefixed resource data.
func ParseResourceFork(b []byte) ([]Resource, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if len(b) < 16 {
		return nil, ErrBadResourceFork
	}
	dataOff := int(bp.BE32(b[0:4]))
	mapOff := int(bp.BE32(b[4:8]))
	dataLen := int(bp.BE32(b[8:12]))
	mapLen := int(bp.BE32(b[12:16]))
	_ = dataLen
	if mapOff < 0 || mapOff+28 > len(b) || mapLen < 0 {
		return nil, ErrBadResourceFork
	}

	// Resource map: 24 bytes (header copy + next-handle + file-ref + fork-attrs) then
	// the type-list and name-list offsets (relative to map start).
	m := b[mapOff:]
	if len(m) < 28 {
		return nil, ErrBadResourceFork
	}
	typeListOff := int(bp.BE16(m[24:26]))
	nameListOff := int(bp.BE16(m[26:28]))
	if typeListOff < 0 || typeListOff+2 > len(m) {
		return nil, ErrBadResourceFork
	}

	typeList := m[typeListOff:]
	numTypes := int(bp.BE16(typeList[0:2])) + 1 // stored as count-1
	var out []Resource
	for i := 0; i < numTypes; i++ {
		base := 2 + i*8
		if base+8 > len(typeList) {
			return nil, ErrBadResourceFork
		}
		var rtype [4]byte
		copy(rtype[:], typeList[base:base+4])
		count := int(bp.BE16(typeList[base+4:base+6])) + 1 // count-1
		refOff := int(bp.BE16(typeList[base+6 : base+8]))  // from start of type list

		for j := 0; j < count; j++ {
			r := refOff + j*12
			if r+12 > len(typeList) {
				return nil, ErrBadResourceFork
			}
			ref := typeList[r : r+12]
			rid := int16(bp.BE16(ref[0:2]))
			nameOff := int(bp.BE16(ref[2:4]))
			mixed := bp.BE32(ref[4:8]) // attrs(1) | dataOffset(3)
			attribs := byte(mixed >> 24)
			rdataOff := int(mixed & 0x00FFFFFF)

			res := Resource{Type: rtype, ID: rid, Attribs: attribs}

			// Resource name (optional): pascal string in the name list.
			if nameOff != 0xFFFF {
				no := nameListOff + nameOff
				if no >= 0 && no < len(m) {
					nl := int(m[no])
					if no+1+nl <= len(m) {
						res.Name = string(m[no+1 : no+1+nl])
						res.HasName = true
					}
				}
			}

			// Resource data: 4-byte length prefix at dataOff+rdataOff, then bytes.
			d := dataOff + rdataOff
			if d+4 > len(b) {
				return nil, ErrBadResourceFork
			}
			n := int(bp.BE32(b[d : d+4]))
			if d+4+n > len(b) {
				return nil, ErrBadResourceFork
			}
			res.Data = append([]byte(nil), b[d+4:d+4+n]...)
			out = append(out, res)
		}
	}
	return out, nil
}

// BuildResourceFork encodes resources into a binary resource fork (the inverse of
// ParseResourceFork; mirrors the Python make_file). Layout: data section (each resource
// length-prefixed), then the map (type list + reference lists + name list).
func BuildResourceFork(res []Resource) []byte {
	// Group by type, preserving first-seen order.
	type group struct {
		rtype [4]byte
		items []int // indices into res
	}
	var groups []group
	idx := map[[4]byte]int{}
	for i, r := range res {
		gi, ok := idx[r.Type]
		if !ok {
			gi = len(groups)
			idx[r.Type] = gi
			groups = append(groups, group{rtype: r.Type})
		}
		groups[gi].items = append(groups[gi].items, i)
	}

	// 1. Data section: length-prefixed resource data; record each resource's offset.
	var data []byte
	dataOffsetOf := make([]int, len(res))
	for i := range res {
		dataOffsetOf[i] = len(data)
		data = bp.AppendBE32(data, uint32(len(res[i].Data)))
		data = append(data, res[i].Data...)
	}

	// 2. Name list + per-resource name offsets (0xFFFF when unnamed).
	var nameList []byte
	nameOffsetOf := make([]int, len(res))
	for i := range res {
		if !res[i].HasName {
			nameOffsetOf[i] = 0xFFFF
			continue
		}
		nameOffsetOf[i] = len(nameList)
		nm := res[i].Name
		if len(nm) > 255 {
			nm = nm[:255]
		}
		nameList = append(nameList, byte(len(nm)))
		nameList = append(nameList, nm...)
	}

	// 3. Type list + reference lists. The type list is: 2-byte (numTypes-1), then 8
	//    bytes per type; the reference lists follow contiguously after it.
	typeListHeader := 2 + len(groups)*8
	var refLists []byte
	refOffsetOf := make([]int, len(groups)) // ref-list offset (from type-list start) per group
	for gi := range groups {
		refOffsetOf[gi] = typeListHeader + len(refLists)
		for _, ri := range groups[gi].items {
			ref := make([]byte, 12)
			bp.PutBE16(ref[0:2], uint16(res[ri].ID))
			bp.PutBE16(ref[2:4], uint16(nameOffsetOf[ri]))
			mixed := uint32(res[ri].Attribs)<<24 | uint32(dataOffsetOf[ri]&0x00FFFFFF)
			bp.PutBE32(ref[4:8], mixed)
			// bytes 8..12 = reserved handle, left zero.
			refLists = append(refLists, ref...)
		}
	}

	typeList := make([]byte, typeListHeader)
	bp.PutBE16(typeList[0:2], uint16(len(groups)-1))
	for gi := range groups {
		base := 2 + gi*8
		copy(typeList[base:base+4], groups[gi].rtype[:])
		bp.PutBE16(typeList[base+4:base+6], uint16(len(groups[gi].items)-1))
		bp.PutBE16(typeList[base+6:base+8], uint16(refOffsetOf[gi]))
	}
	typeList = append(typeList, refLists...)

	// 4. Assemble the map: 24-byte (mostly-zero) header area, then the type-list and
	//    name-list offsets, then the type list, then the name list.
	const mapPrefix = 28 // 24 reserved + 2 type-list-off + 2 name-list-off
	typeListOff := mapPrefix
	nameListOff := mapPrefix + len(typeList)
	mapBytes := make([]byte, mapPrefix)
	bp.PutBE16(mapBytes[24:26], uint16(typeListOff))
	bp.PutBE16(mapBytes[26:28], uint16(nameListOff))
	mapBytes = append(mapBytes, typeList...)
	mapBytes = append(mapBytes, nameList...)

	// 5. Header (16 bytes) + data + map. Data starts right after the header.
	const headerLen = 16
	dataOff := headerLen
	mapOff := headerLen + len(data)
	out := make([]byte, headerLen)
	bp.PutBE32(out[0:4], uint32(dataOff))
	bp.PutBE32(out[4:8], uint32(mapOff))
	bp.PutBE32(out[8:12], uint32(len(data)))
	bp.PutBE32(out[12:16], uint32(len(mapBytes)))
	out = append(out, data...)
	out = append(out, mapBytes...)
	return out
}
