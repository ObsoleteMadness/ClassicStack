package ddp

import (
	"encoding/binary"
	"fmt"
)

const MaxDataLength = 586

type Datagram struct {
	HopCount           uint8
	DestinationNetwork uint16
	SourceNetwork      uint16
	DestinationNode    uint8
	SourceNode         uint8
	DestinationSocket  uint8
	SourceSocket       uint8
	DDPType            uint8
	Data               []byte
}

func Checksum(data []byte) uint16 {
	var v uint16
	for _, b := range data {
		v += uint16(b)
		v = (v&0x7FFF)<<1 | (v>>15)&1
	}
	if v == 0 {
		return 0xFFFF
	}
	return v
}

func DatagramFromLongHeaderBytes(data []byte, verifyChecksum bool) (Datagram, error) {
	if len(data) < 13 {
		return Datagram{}, fmt.Errorf("data too short, must be at least 13 bytes")
	}
	first := data[0]
	second := data[1]
	if first&0xC0 != 0 {
		return Datagram{}, fmt.Errorf("invalid long DDP header")
	}
	hop := (first & 0x3C) >> 2
	length := int(first&0x03)<<8 | int(second)
	if length > 13+MaxDataLength || length != len(data) {
		return Datagram{}, fmt.Errorf("invalid long DDP length")
	}
	checksum := binary.BigEndian.Uint16(data[2:4])
	if checksum != 0 && verifyChecksum {
		if got := Checksum(data[4:]); got != checksum {
			return Datagram{}, fmt.Errorf("invalid long DDP checksum 0x%04X != 0x%04X", checksum, got)
		}
	}
	return Datagram{
		HopCount:           hop,
		DestinationNetwork: binary.BigEndian.Uint16(data[4:6]),
		SourceNetwork:      binary.BigEndian.Uint16(data[6:8]),
		DestinationNode:    data[8],
		SourceNode:         data[9],
		DestinationSocket:  data[10],
		SourceSocket:       data[11],
		DDPType:            data[12],
		Data:               append([]byte(nil), data[13:]...),
	}, nil
}

func DatagramFromShortHeaderBytes(destinationNode, sourceNode uint8, data []byte) (Datagram, error) {
	if len(data) < 5 {
		return Datagram{}, fmt.Errorf("data too short, must be at least 5 bytes")
	}
	first := data[0]
	second := data[1]
	if first&0xFC != 0 {
		return Datagram{}, fmt.Errorf("invalid short DDP header")
	}
	length := int(first&0x03)<<8 | int(second)
	if length > 5+MaxDataLength || length != len(data) {
		return Datagram{}, fmt.Errorf("invalid short DDP length")
	}
	return Datagram{
		HopCount:           0,
		DestinationNetwork: 0,
		SourceNetwork:      0,
		DestinationNode:    destinationNode,
		SourceNode:         sourceNode,
		DestinationSocket:  data[2],
		SourceSocket:       data[3],
		DDPType:            data[4],
		Data:               append([]byte(nil), data[5:]...),
	}, nil
}

func (d Datagram) Copy() Datagram {
	x := d
	x.Data = append([]byte(nil), d.Data...)
	return x
}

func (d Datagram) Hop() Datagram {
	x := d.Copy()
	x.HopCount++
	return x
}

func (d Datagram) validate() error {
	if d.HopCount > 15 || d.DestinationNetwork > 65534 || d.SourceNetwork > 65534 || d.SourceNode == 0 || d.SourceNode == 255 {
		return fmt.Errorf("invalid datagram header values")
	}
	if len(d.Data) > MaxDataLength {
		return fmt.Errorf("data length %d exceeds %d", len(d.Data), MaxDataLength)
	}
	return nil
}

func (d Datagram) AsLongHeaderBytes(calculateChecksum bool) ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	payload := make([]byte, 9+len(d.Data))
	binary.BigEndian.PutUint16(payload[0:2], d.DestinationNetwork)
	binary.BigEndian.PutUint16(payload[2:4], d.SourceNetwork)
	payload[4] = d.DestinationNode
	payload[5] = d.SourceNode
	payload[6] = d.DestinationSocket
	payload[7] = d.SourceSocket
	payload[8] = d.DDPType
	copy(payload[9:], d.Data)
	length := 4 + len(payload)
	out := make([]byte, 4+len(payload))
	out[0] = (d.HopCount&0xF)<<2 | uint8((length&0x300)>>8)
	out[1] = uint8(length & 0xFF)
	if calculateChecksum {
		binary.BigEndian.PutUint16(out[2:4], Checksum(payload))
	}
	copy(out[4:], payload)
	return out, nil
}

func (d Datagram) AsShortHeaderBytes() ([]byte, error) {
	if d.HopCount != 0 {
		return nil, fmt.Errorf("short-header datagrams may not have non-zero hop count")
	}
	if err := d.validate(); err != nil {
		return nil, err
	}
	length := 5 + len(d.Data)
	out := make([]byte, 5+len(d.Data))
	out[0] = uint8((length & 0x300) >> 8)
	out[1] = uint8(length & 0xFF)
	out[2] = d.DestinationSocket
	out[3] = d.SourceSocket
	out[4] = d.DDPType
	copy(out[5:], d.Data)
	return out, nil
}
