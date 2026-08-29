package finder

import (
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

func TestRememberPublishesNetworks(t *testing.T) {
	b := bus.New(8)
	ch, cancel := b.Subscribe(bus.TopicFinder)
	defer cancel()
	s := New(nil, nil)
	s.SetPublisher(b)

	s.remember(KindAFP, []VolumeInfo{
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
	})

	select {
	case ev := <-ch:
		fu, ok := ev.(bus.FinderUpdated)
		if !ok {
			t.Fatalf("event is %T, want FinderUpdated", ev)
		}
		if fu.Kind != bus.FinderKindNetworks || fu.Scheme != KindAFP || len(fu.Volumes) != 1 {
			t.Fatalf("event = %+v", fu)
		}
		if fu.Volumes[0].Title != "Mac" {
			t.Fatalf("volume = %+v", fu.Volumes[0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for finder networks event")
	}
}

func TestRememberSkipsUnchangedNetworks(t *testing.T) {
	b := bus.New(8)
	ch, cancel := b.Subscribe(bus.TopicFinder)
	defer cancel()
	s := New(nil, nil)
	s.SetPublisher(b)

	vols := []VolumeInfo{{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"}}
	s.remember(KindAFP, vols)
	<-ch

	s.remember(KindAFP, append([]VolumeInfo(nil), vols...))
	select {
	case ev := <-ch:
		t.Fatalf("published duplicate list: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRememberPublishesWhenListChanges(t *testing.T) {
	b := bus.New(8)
	ch, cancel := b.Subscribe(bus.TopicFinder)
	defer cancel()
	s := New(nil, nil)
	s.SetPublisher(b)

	s.remember(KindAFP, []VolumeInfo{{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"}})
	<-ch

	s.remember(KindAFP, []VolumeInfo{
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
		{ID: "afp://Plus,tcp/", Kind: KindAFP, Title: "Plus"},
	})
	select {
	case ev := <-ch:
		fu := ev.(bus.FinderUpdated)
		if len(fu.Volumes) != 2 {
			t.Fatalf("volumes = %+v, want 2", fu.Volumes)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for updated networks event")
	}
}

func TestRememberSkipsReorderedSameSet(t *testing.T) {
	b := bus.New(8)
	ch, cancel := b.Subscribe(bus.TopicFinder)
	defer cancel()
	s := New(nil, nil)
	s.SetPublisher(b)

	s.remember(KindAFP, []VolumeInfo{
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
		{ID: "afp://Plus,tcp/", Kind: KindAFP, Title: "Plus"},
	})
	<-ch

	// Same two servers, opposite order — as concurrent per-interface scan
	// goroutines can produce across two scanLoop ticks of an unchanged network.
	s.remember(KindAFP, []VolumeInfo{
		{ID: "afp://Plus,tcp/", Kind: KindAFP, Title: "Plus"},
		{ID: "afp://Mac,ltoudp/", Kind: KindAFP, Title: "Mac"},
	})
	select {
	case ev := <-ch:
		t.Fatalf("published a reordered-but-unchanged list: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishScanningNilPublisher(t *testing.T) {
	s := New(nil, nil)
	s.publishScanning(true) // must not panic
}
