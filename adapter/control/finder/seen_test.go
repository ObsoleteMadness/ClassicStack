package finder

import "testing"

func TestLastSeenEmpty(t *testing.T) {
	svc := New(nil, nil)
	if got := svc.LastSeen(""); len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
	if got := svc.LastSeen(KindAFP); len(got) != 0 {
		t.Fatalf("scheme got %+v, want empty", got)
	}
}

func TestRememberReplacesSchemeAndKeepsOthers(t *testing.T) {
	svc := New(nil, nil)
	svc.remember(KindAFP, []VolumeInfo{
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
	})
	svc.remember(KindSMB, []VolumeInfo{
		{ID: "smb://FILE,tcp/", Kind: KindSMB, Title: "FILE"},
	})
	svc.remember(KindAFP, []VolumeInfo{
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
		{ID: "afp://Plus,tcp/", Kind: KindAFP, Title: "Plus"},
	})

	afp := svc.LastSeen(KindAFP)
	if len(afp) != 2 || afp[0].Title != "Mac" || afp[1].Title != "Plus" {
		t.Fatalf("afp = %+v", afp)
	}
	smb := svc.LastSeen(KindSMB)
	if len(smb) != 1 || smb[0].Title != "FILE" {
		t.Fatalf("smb = %+v", smb)
	}
	all := svc.LastSeen("")
	if len(all) != 3 {
		t.Fatalf("all = %+v, want 3", all)
	}
}

func TestRememberEmptyScanClearsScheme(t *testing.T) {
	svc := New(nil, nil)
	svc.remember(KindAFP, []VolumeInfo{{ID: "afp://Gone,ltoudp/", Kind: KindAFP, Title: "Gone"}})
	svc.remember(KindAFP, nil)
	if got := svc.LastSeen(KindAFP); len(got) != 0 {
		t.Fatalf("got %+v, want empty after empty scan", got)
	}
}

func TestDiscoverMissKeepsLastSeen(t *testing.T) {
	svc := New(nil, nil)
	svc.remember(KindAFP, []VolumeInfo{{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"}})
	got, err := svc.Discover(DiscoverRequest{Scheme: "not-a-scheme"})
	if err == nil {
		t.Fatal("want unknown scheme error")
	}
	if got != nil {
		t.Fatalf("got %+v, want nil on unknown scheme", got)
	}
	if cached := svc.LastSeen(KindAFP); len(cached) != 1 || cached[0].Title != "Mac" {
		t.Fatalf("last-seen clobbered: %+v", cached)
	}
}
