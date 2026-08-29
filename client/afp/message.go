package afp

import (
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
	aspproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// AFP pop-up kinds delivered through client.Options.OnServerMessage.
const (
	popupLogin  = "login"  // FPGetSrvrMsg type 0, fetched unprompted after FPOpenVol
	popupServer = "server" // FPGetSrvrMsg type 1, fetched after an AspAttnMsg attention
)

// fetchLoginMessage requests the login greeting (FPGetSrvrMsg type 0) when the
// server advertised SupportsSrvrMsg. Classic Finder does this unprompted right
// after FPOpenVol and shows the text once per mount. An empty reply is a no-op.
func (f *FS) fetchLoginMessage() {
	if !f.srvInfo.SupportsSrvrMsg() {
		return
	}
	msg, err := f.getSrvrMsg(proto.SrvrMsgTypeLogin)
	if err != nil {
		afpLog.Log1(log.Debug, "FPGetSrvrMsg login failed", log.Str("err", err.Error()))
		return
	}
	f.emitMessage(popupLogin, msg)
}

// installAttentionHandler registers on sess so an AspAttnMsg attention fetches
// the pending server message (FPGetSrvrMsg type 1) and delivers it. The handler
// runs off the WSS loop (ASP starts a goroutine) so the subsequent Command
// cannot stall attentions / tickles / WriteContinue.
func (f *FS) installAttentionHandler() {
	sess, _ := f.session()
	if sess == nil {
		return
	}
	sess.SetAttentionHandler(f.handleAttention)
}

// handleAttention is the ASP attention callback. A message-waiting attention
// (AspAttnMsg) is followed by FPGetSrvrMsg type 1, matching observed AppleShare
// clients. Other attention bits (shutdown, no-reconnect) are logged; the server
// ends the session itself with CloseSession.
func (f *FS) handleAttention(code uint16) {
	afpLog.Log1(log.Debug, "ASP attention", log.Int("code", int64(code)))
	if code&aspproto.AspAttnMsg == 0 {
		return
	}
	msg, err := f.getSrvrMsg(proto.SrvrMsgTypeServer)
	if err != nil {
		afpLog.Log1(log.Debug, "FPGetSrvrMsg server failed", log.Str("err", err.Error()))
		return
	}
	f.emitMessage(popupServer, msg)
}

// getSrvrMsg runs FPGetSrvrMsg and returns the MacRoman message decoded to UTF-8.
func (f *FS) getSrvrMsg(msgType uint16) (string, error) {
	body, err := f.command("FPGetSrvrMsg", "", func(uint16) []byte {
		return proto.GetSrvrMsgRequest{Type: msgType, Bitmap: proto.SrvrMsgBitmapText}.Marshal()
	})
	if err != nil {
		return "", err
	}
	reply, ok := proto.ParseGetSrvrMsgReply(body)
	if !ok {
		return "", errMalformed("FPGetSrvrMsg reply")
	}
	text := afpDecodeName(reply.Message)
	afpLog.Log(log.Debug, "FPGetSrvrMsg",
		log.Int("type", int64(msgType)),
		log.Str("text", text))
	return text, nil
}

// emitMessage delivers a non-empty pop-up to OnServerMessage.
func (f *FS) emitMessage(kind, text string) {
	if text == "" || f.onMessage == nil {
		return
	}
	from := f.srvInfo.ServerName
	if from == "" {
		from = f.name
	}
	f.onMessage(kind, from, text)
}
