package ethertalk

import (
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/internal/portbase"
)

// Name is the component/section key for the EtherTalk port.
const Name = "EtherTalk"

// New builds the Phase 1 EtherTalk placeholder port from a section and an
// optional frame link. The data path is inert; Phase 2 replaces this with a real
// port that frames DDP over the Ethernet link and runs AARP.
func New(sec *portbase.Section, frame link.FrameLink, logger log.Logger) *portbase.Port {
	if sec == nil {
		sec = &portbase.Section{}
	}
	sec.SKey = Name
	return portbase.New(sec, frame, logger)
}
