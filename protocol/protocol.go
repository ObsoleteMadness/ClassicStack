// Package protocol defines cross-protocol contracts used by OmniTalk's wire
// implementations (DDP, ATP, ASP, ZIP, RTMP, AEP, LLAP, NBP). Each protocol
// lives in its own subpackage; this package carries only interfaces common to
// all of them.
package protocol

// Packet is the contract implemented by any AppleTalk protocol header or
// datagram that supports binary wire encoding/decoding and structured log
// formatting.
type Packet interface {
	String() string
	Marshal() []byte
	Unmarshal(data []byte) error
}
