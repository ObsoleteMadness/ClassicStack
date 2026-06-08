package netbeui

import (
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/internal/portbase"
)

// Name is the component/section key for the NetBEUI port.
const Name = "NetBEUI"

// New builds the Phase 1 NetBEUI placeholder port. The data path is inert; Phase
// 2 replaces it with real NetBEUI framing as a NetBIOS transport.
func New(sec *portbase.Section, frame link.FrameLink, logger log.Logger) *portbase.Port {
	if sec == nil {
		sec = &portbase.Section{}
	}
	sec.SKey = Name
	return portbase.New(sec, frame, logger)
}
