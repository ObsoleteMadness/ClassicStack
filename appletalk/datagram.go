package appletalk

import "github.com/pgodw/omnitalk/go/protocol/ddp"

const MaxDataLength = ddp.MaxDataLength

type Datagram = ddp.Datagram

func DDPChecksum(data []byte) uint16 { return ddp.Checksum(data) }

func DatagramFromLongHeaderBytes(data []byte, verifyChecksum bool) (Datagram, error) {
	return ddp.DatagramFromLongHeaderBytes(data, verifyChecksum)
}

func DatagramFromShortHeaderBytes(destinationNode, sourceNode uint8, data []byte) (Datagram, error) {
	return ddp.DatagramFromShortHeaderBytes(destinationNode, sourceNode, data)
}
