package archive

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

func isMacBinary(data []byte) bool {
	if len(data) < 128 {
		return false
	}
	return data[0] == 0 && data[74] == 0 && data[82] == 0 && data[122] == 129
}

func expandMacBinary(data []byte) (*Node, error) {
	if !isMacBinary(data) {
		return nil, ErrUnsupportedFormat
	}
	nameLen := int(data[1])
	if nameLen < 1 || nameLen > 63 {
		return nil, ErrCorrupt
	}
	dataLen := int(bp.BE32(data[83:87]))
	rsrcLen := int(bp.BE32(data[87:91]))
	off := 128
	dataEnd := off + dataLen
	if dataEnd > len(data) {
		return nil, ErrCorrupt
	}
	fileData := append([]byte(nil), data[off:dataEnd]...)
	off = align128(dataEnd)
	rsrcEnd := off + rsrcLen
	if rsrcEnd > len(data) {
		return nil, ErrCorrupt
	}
	var fi [32]byte
	copy(fi[0:4], data[65:69])
	copy(fi[4:8], data[69:73])
	fi[8] = data[73]
	name := string(data[2 : 2+nameLen])
	return &Node{
		Name:       name,
		Data:       fileData,
		Resource:   append([]byte(nil), data[off:rsrcEnd]...),
		FinderInfo: fi,
	}, nil
}

func align128(n int) int {
	if r := n % 128; r != 0 {
		return n + (128 - r)
	}
	return n
}
