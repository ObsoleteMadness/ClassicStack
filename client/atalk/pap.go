package atalk

import (
	"fmt"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/pap"
)

// pap.go is the client-side PAP status query over the ATP requester: a single
// SendStatus/Status transaction (Inside AppleTalk, 2nd ed., ch. 10) against a server's
// session listening socket (SLS), with no PAP connection opened. This is how a Chooser
// polls a printer's status line without a PAPOpen — the minimal PAP interaction there is,
// so csnbp + this is enough to enumerate printer shares and show each one's status
// without implementing PAPOpen/data-transfer/Tickle/PAPClose at all.

// PAPPrinterType is the NBP type a shared LaserWriter-compatible printer conventionally
// registers its SLS under. Inside AppleTalk ch. 10 has no single canonical PAP type —
// PAP itself is name-agnostic — but "LaserWriter" is what the Mac's Chooser (and most
// third-party PAP spoolers) use so LaserWriter-driver clients can find them.
const PAPPrinterType = "LaserWriter"

// papStatusDataOffset is where the Pascal status string starts in a Status reply's ATP
// data area: 4 reserved/unused bytes precede it (Inside AppleTalk ch. 10, fig. 10-6).
const papStatusDataOffset = 4

// PAPStatus sends a PAP SendStatus request to dst (a server's SLS address, typically from
// an NBP lookup) and returns its decoded status string. It does not open a connection —
// SendStatus is answered directly from the server's last SLInit/HeresStatus string, so
// this is safe to call against every match from an NBP browse without disturbing any
// print job in progress.
func (a *ATP) PAPStatus(dst Addr, timeout time.Duration) (string, error) {
	if timeout > 0 {
		a.SetRetryPolicy(timeout, 1)
		defer a.SetRetryPolicy(0, 0)
	}

	h := pap.Header{Function: pap.FuncSendStatus}
	resp, err := a.Request(dst, h.Encode(), nil, false, 1)
	if err != nil {
		return "", err
	}
	rh, _ := pap.ParseHeader(resp.UserData)
	if rh.Function != pap.FuncStatus {
		return "", fmt.Errorf("atalk: unexpected PAP reply function %d (want Status)", rh.Function)
	}
	if len(resp.Data) < papStatusDataOffset+1 {
		return "", fmt.Errorf("atalk: PAP status reply too short (%d bytes)", len(resp.Data))
	}
	n := int(resp.Data[papStatusDataOffset])
	str := resp.Data[papStatusDataOffset+1:]
	if len(str) < n {
		return "", fmt.Errorf("atalk: PAP status string truncated (want %d, got %d)", n, len(str))
	}
	return encoding.MacRomanToUTF8(str[:n]), nil
}
