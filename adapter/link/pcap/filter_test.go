package pcap

import "testing"

func TestExcludeSelf(t *testing.T) {
	mac := [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}

	tests := []struct {
		name   string
		filter string
		mac    [6]byte
		want   string
	}{
		{
			name:   "zero mac is a no-op",
			filter: "ipx",
			mac:    [6]byte{},
			want:   "ipx",
		},
		{
			name:   "combines with a non-empty filter",
			filter: "ipx",
			mac:    mac,
			want:   "(ipx) and not (ether src 00:11:22:33:44:55)",
		},
		{
			name:   "empty filter yields the exclusion alone",
			filter: "",
			mac:    mac,
			want:   "not (ether src 00:11:22:33:44:55)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExcludeSelf(tt.filter, tt.mac); got != tt.want {
				t.Fatalf("ExcludeSelf(%q, %v) = %q, want %q", tt.filter, tt.mac, got, tt.want)
			}
		})
	}
}
