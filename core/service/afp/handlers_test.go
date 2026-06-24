package afp

import "testing"

// TestSat32 proves the AFP volume byte fields SATURATE at 4 GiB − 1 instead of
// wrapping: a vintage AFP 2.x client must see "full" for any disk larger than the
// 32-bit field can express, never a wrapped (smaller, wrong) figure.
func TestSat32(t *testing.T) {
	const max = uint64(0xFFFFFFFF)
	cases := []struct {
		name string
		in   uint64
		want uint32
	}{
		{"zero", 0, 0},
		{"small", 1 << 20, 1 << 20},                   // 1 MiB passes through
		{"just-under-cap", max - 1, uint32(max - 1)},  // exact value kept
		{"at-cap", max, afpMaxVolumeBytes},            // 4 GiB − 1 kept
		{"over-cap-6GiB", 6 << 30, afpMaxVolumeBytes}, // 6 GiB → capped, NOT wrapped to 2 GiB
		{"huge-1TiB", 1 << 40, afpMaxVolumeBytes},     // 1 TiB → capped
	}
	for _, tc := range cases {
		if got := sat32(tc.in); got != tc.want {
			t.Errorf("sat32(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
