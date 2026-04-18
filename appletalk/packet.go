package appletalk

// Packet is a generic protocol packet contract used by protocol layers that
// support binary wire encoding/decoding and structured log formatting.
type Packet interface {
	String() string
	Marshal() []byte
	Unmarshal(data []byte) error
}
