package asp

import "testing"

// TestNextSeqStartsAtZero is the regression for the connect stall after login: a real
// System 7.x ASP server tracks the expected request sequence and SILENTLY DROPS a
// Command whose sequence it did not expect. Ground truth (captures/vmac-to-vmac.pcapng):
// the real Mac workstation's first Command is sequence 0, then 1, 2, … The client
// previously started at 1, so every Command went unanswered.
func TestNextSeqStartsAtZero(t *testing.T) {
	s := &Session{}
	for i, want := range []uint16{0, 1, 2, 3} {
		if got := s.nextSeq(); got != want {
			t.Fatalf("nextSeq call %d = %d, want %d", i, got, want)
		}
	}
}
