//go:build afp || all

package asp

import "testing"

func TestSeqFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		seq  uint16
		tid  uint16
		want bool
	}{
		{"first message accepted", 0, 100, true},
		{"new seq accepted", 1, 101, true},
		{"same seq same tid is ATP retransmit, accepted", 1, 101, true},
		{"same seq new tid is ASP duplicate, dropped", 1, 102, false},
		{"after duplicate, advancing seq accepted", 2, 103, true},
		{"seqNum wraparound back to 0 accepted", 0, 104, true},
	}
	var f seqFilter
	for _, tc := range tests {
		if got := f.accept(tc.seq, tc.tid); got != tc.want {
			t.Errorf("%s: accept(%d, %d) = %v, want %v", tc.name, tc.seq, tc.tid, got, tc.want)
		}
	}
}
