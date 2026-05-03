package localtalk

import "github.com/ObsoleteMadness/ClassicStack/protocol/llap"

// LLAP wire-format types and codes have moved to protocol/llap.
// These aliases keep existing port-internal call sites unchanged while
// new code (service/llap, tests) imports protocol/llap directly.

const (
	LLAPTypeAppleTalkShortHeader = llap.TypeAppleTalkShortHeader
	LLAPTypeAppleTalkLongHeader  = llap.TypeAppleTalkLongHeader
	LLAPTypeENQ                  = llap.TypeENQ
	LLAPTypeACK                  = llap.TypeACK
	LLAPTypeRTS                  = llap.TypeRTS
	LLAPTypeCTS                  = llap.TypeCTS

	LLAPBroadcastNode = llap.BroadcastNode
	LLAPMaxDataSize   = llap.MaxDataSize
)

type LLAPFrame = llap.Frame

func LLAPFrameFromBytes(b []byte) (LLAPFrame, error) { return llap.FrameFromBytes(b) }
