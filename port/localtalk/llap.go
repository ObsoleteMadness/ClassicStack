package localtalk

import "fmt"

const (
	LLAPTypeAppleTalkShortHeader = 0x01
	LLAPTypeAppleTalkLongHeader  = 0x02
	LLAPTypeENQ                  = 0x81
	LLAPTypeACK                  = 0x82
	LLAPTypeRTS                  = 0x84
	LLAPTypeCTS                  = 0x85

	LLAPBroadcastNode = 0xFF
	LLAPMaxDataSize   = 600
)

type LLAPFrame struct {
	DestinationNode uint8
	SourceNode      uint8
	Type            uint8
	Payload         []byte
}

func LLAPFrameFromBytes(frame []byte) (LLAPFrame, error) {
	if len(frame) < 3 {
		return LLAPFrame{}, fmt.Errorf("LLAP frame too short: %d", len(frame))
	}
	x := LLAPFrame{
		DestinationNode: frame[0],
		SourceNode:      frame[1],
		Type:            frame[2],
		Payload:         append([]byte(nil), frame[3:]...),
	}
	if err := x.Validate(); err != nil {
		return LLAPFrame{}, err
	}
	return x, nil
}

func (f LLAPFrame) Validate() error {
	if f.IsControl() {
		if len(f.Payload) != 0 {
			return fmt.Errorf("LLAP control frame 0x%02X has payload length %d", f.Type, len(f.Payload))
		}
		switch f.Type {
		case LLAPTypeENQ, LLAPTypeACK, LLAPTypeRTS, LLAPTypeCTS:
			return nil
		default:
			return fmt.Errorf("invalid LLAP control type 0x%02X", f.Type)
		}
	}
	if !f.IsData() {
		return fmt.Errorf("invalid LLAP frame type 0x%02X", f.Type)
	}
	if len(f.Payload) > LLAPMaxDataSize {
		return fmt.Errorf("LLAP payload too large: %d", len(f.Payload))
	}
	return nil
}

func (f LLAPFrame) IsControl() bool { return f.Type >= 0x80 }

func (f LLAPFrame) IsData() bool {
	return f.Type == LLAPTypeAppleTalkShortHeader || f.Type == LLAPTypeAppleTalkLongHeader
}

func (f LLAPFrame) Bytes() []byte {
	out := make([]byte, 0, 3+len(f.Payload))
	out = append(out, f.DestinationNode, f.SourceNode, f.Type)
	out = append(out, f.Payload...)
	return out
}
