package afp

// Classic Mac OS resource-fork parsing. Used by the AFP Desktop database
// ingestion path to pull ICN# bitmaps out of BNDL/FREF chains inside
// AppleDouble resource forks.

import (
	"encoding/binary"
	"errors"
)

// resourceForkResource is one decoded resource from a classic Mac resource
// fork: its 4-char type, signed 16-bit ID, and raw data bytes.
type resourceForkResource struct {
	resType [4]byte
	resID   int16
	data    []byte
}

// parseResourceFork decodes a classic Mac resource fork blob into a flat list
// of resources. Malformed or truncated structures are tolerated — anything
// successfully parsed is returned, and the first hard structural error causes
// an error return.
func parseResourceFork(b []byte) ([]resourceForkResource, error) {
	if len(b) < 16 {
		return nil, errors.New("resource fork too small")
	}
	dataOff := binary.BigEndian.Uint32(b[0:4])
	mapOff := binary.BigEndian.Uint32(b[4:8])
	dataLen := binary.BigEndian.Uint32(b[8:12])
	mapLen := binary.BigEndian.Uint32(b[12:16])
	if uint64(mapOff)+uint64(mapLen) > uint64(len(b)) {
		return nil, errors.New("resource map out of range")
	}
	if uint64(dataOff)+uint64(dataLen) > uint64(len(b)) {
		return nil, errors.New("resource data out of range")
	}
	mp := b[mapOff : uint64(mapOff)+uint64(mapLen)]
	if len(mp) < 30 {
		return nil, errors.New("resource map header too small")
	}
	// Resource map layout (relative to mp):
	//   [0:16]  copy of header
	//   [16:20] next resource map handle (unused on disk)
	//   [20:22] file reference number
	//   [22:24] attributes
	//   [24:26] offset to type list (from start of map)
	//   [26:28] offset to name list (from start of map)
	typeListOff := binary.BigEndian.Uint16(mp[24:26])
	if int(typeListOff) >= len(mp) {
		return nil, errors.New("type list offset out of range")
	}
	tl := mp[typeListOff:]
	if len(tl) < 2 {
		return nil, errors.New("type list truncated")
	}
	numTypesM1 := binary.BigEndian.Uint16(tl[0:2])
	numTypes := int(numTypesM1) + 1

	var out []resourceForkResource
	for i := 0; i < numTypes; i++ {
		entryOff := 2 + i*8
		if entryOff+8 > len(tl) {
			break
		}
		var resType [4]byte
		copy(resType[:], tl[entryOff:entryOff+4])
		numRefsM1 := binary.BigEndian.Uint16(tl[entryOff+4 : entryOff+6])
		refOff := binary.BigEndian.Uint16(tl[entryOff+6 : entryOff+8])
		refBase := int(typeListOff) + int(refOff)
		numRefs := int(numRefsM1) + 1
		for j := 0; j < numRefs; j++ {
			rOff := refBase + j*12
			if rOff+12 > len(mp) {
				break
			}
			resID := int16(binary.BigEndian.Uint16(mp[rOff : rOff+2]))
			// rOff+2: name offset (ignored)
			// rOff+4: 1 byte attrs + 3 byte data offset (big-endian, packed).
			packed := binary.BigEndian.Uint32(mp[rOff+4 : rOff+8])
			dOff := packed & 0x00FFFFFF
			abs := uint64(dataOff) + uint64(dOff)
			if abs+4 > uint64(len(b)) {
				continue
			}
			dl := binary.BigEndian.Uint32(b[abs : abs+4])
			if abs+4+uint64(dl) > uint64(len(b)) {
				continue
			}
			data := append([]byte(nil), b[abs+4:abs+4+uint64(dl)]...)
			out = append(out, resourceForkResource{
				resType: resType,
				resID:   resID,
				data:    data,
			})
		}
	}
	return out, nil
}
