// Package localtalk implements LocalTalk (AppleTalk Phase 1) as an
// OmniTalk port.
//
// LLAP frames travel over one of several physical/virtual transports
// implemented in subpackages: LToUDP (UDP multicast on
// 239.192.76.84:1954), TashTalk (serial-attached hardware at 1 Mbit/s),
// and a virtual loopback for tests.
package localtalk
