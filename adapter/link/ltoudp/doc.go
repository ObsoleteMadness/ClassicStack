// Package ltoudp is the LToUDP FrameLink adapter (§2, M10): LocalTalk frames
// tunnelled over an IPv4 multicast group (239.192.76.84:1954), the de-facto
// "LocalTalk over UDP" simulated segment that Mini vMac, BasiliskII, and other
// emulators share.
//
// On the wire every datagram is a 4-byte sender ID followed by the raw LLAP
// frame. The sender ID lets a participant ignore its own multicast echo (the
// group has loopback enabled so every sender also receives its own packets);
// it is NOT part of the LLAP/DDP framing and is stripped on Read / prepended on
// Write here, so the LLAP framer above this adapter (adapter/link/framing.
// LocalTalk) sees clean LLAP frames.
//
// Ring: adapter. May import net + golang.org/x/net/ipv4 + golang.org/x/sys;
// presents only the core/link.FrameLink interface upward. The archtest gate
// (A2) forbids net under core/, which is exactly why this lives here.
package ltoudp
