//go:build darwin

package ltoudp

import "net"

// triggerLocalNetworkPrivacyAlert performs a local-network multicast operation
// so macOS 15+ can present the Local Network prompt (TN3179). Connecting a UDP
// socket to the LToUDP group does not send a datagram; it is enough for TCC to
// attribute the process (or its responsible app) and record the user's choice.
//
// A command-line tool started from Terminal or SSH is auto-allowed. A binary
// spawned by another app (Cursor, Finder, a LaunchAgent that is not a daemon)
// uses that app's Local Network privilege — the prompt names the parent, not
// ClassicStack, unless this executable has an embedded Info.plist.
func triggerLocalNetworkPrivacyAlert() {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{
		IP:   net.ParseIP(GroupAddr),
		Port: 9, // discard; connect-only, no payload
	})
	if err != nil {
		return
	}
	_ = c.Close()
}
