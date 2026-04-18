package localtalk

import (
	"net"
	"testing"
)

func TestShouldTryJoinInterface(t *testing.T) {
	tests := []struct {
		name            string
		flags           net.Flags
		includeLoopback bool
		hasIPv4         bool
		connectedKnown  bool
		connected       bool
		want            bool
	}{
		{
			name:            "eligible non-loopback interface",
			flags:           net.FlagUp | net.FlagMulticast,
			includeLoopback: false,
			hasIPv4:         true,
			want:            true,
		},
		{
			name:            "skips disconnected interface when status known",
			flags:           net.FlagUp | net.FlagMulticast,
			includeLoopback: false,
			hasIPv4:         true,
			connectedKnown:  true,
			connected:       false,
			want:            false,
		},
		{
			name:            "allows interface when status unknown",
			flags:           net.FlagUp | net.FlagMulticast,
			includeLoopback: false,
			hasIPv4:         true,
			connectedKnown:  false,
			connected:       false,
			want:            true,
		},
		{
			name:            "skips loopback in non-loopback pass",
			flags:           net.FlagUp | net.FlagMulticast | net.FlagLoopback,
			includeLoopback: false,
			hasIPv4:         true,
			want:            false,
		},
		{
			name:            "requires ipv4 address",
			flags:           net.FlagUp | net.FlagMulticast,
			includeLoopback: false,
			hasIPv4:         false,
			want:            false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intf := &net.Interface{Flags: tc.flags}
			got := shouldTryJoinInterface(intf, tc.includeLoopback, tc.hasIPv4, tc.connectedKnown, tc.connected)
			if got != tc.want {
				t.Fatalf("shouldTryJoinInterface() = %v, want %v", got, tc.want)
			}
		})
	}
}
