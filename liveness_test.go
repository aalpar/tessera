package tessera

import (
	"slices"
	"testing"
)

func TestStalenessTrackerDetectsDeadWorker(t *testing.T) {
	ring := NewRing("tracker")
	ring.Join("worker-A")
	ring.Join("worker-B")

	wA := NewWorker("worker-A")
	wB := NewWorker("worker-B")

	// Both workers produce events.
	deltasA := wA.BackupFile("file-1", []string{"H1"})
	deltasB := wB.BackupFile("file-2", []string{"H2"})

	// Sync into a shared index (simulating the tracker's view).
	shared := New("tracker")
	for _, d := range deltasA {
		shared.Merge(d)
	}
	for _, d := range deltasB {
		shared.Merge(d)
	}

	tracker := NewStalenessTracker(3)

	// Round 1: both workers' events are visible.
	stale := tracker.Observe(ring, shared.Context())
	if len(stale) != 0 {
		t.Fatalf("round 1: expected no stale members, got %v", stale)
	}

	// Worker-B stops producing events. Worker-A continues.
	for i := 0; i < 3; i++ {
		d := wA.BackupFile("file-1", []string{"H3"})
		for _, dd := range d {
			shared.Merge(dd)
		}
		stale = tracker.Observe(ring, shared.Context())
	}

	// After 3 rounds of no advancement, worker-B should be stale.
	if len(stale) != 1 || stale[0] != "worker-B" {
		t.Fatalf("expected worker-B stale, got %v", stale)
	}
}

func TestStalenessTrackerResetsOnActivity(t *testing.T) {
	ring := NewRing("tracker")
	ring.Join("worker-A")

	w := NewWorker("worker-A")
	shared := New("tracker")

	// Initial event.
	for _, d := range w.BackupFile("file-1", []string{"H1"}) {
		shared.Merge(d)
	}

	tracker := NewStalenessTracker(3)
	tracker.Observe(ring, shared.Context())

	// 2 rounds of silence (below threshold).
	tracker.Observe(ring, shared.Context())
	tracker.Observe(ring, shared.Context())

	// Worker produces a new event — resets the counter.
	for _, d := range w.BackupFile("file-2", []string{"H2"}) {
		shared.Merge(d)
	}
	stale := tracker.Observe(ring, shared.Context())
	if len(stale) != 0 {
		t.Fatalf("expected no stale after activity, got %v", stale)
	}

	// 3 more rounds of silence.
	for i := 0; i < 3; i++ {
		stale = tracker.Observe(ring, shared.Context())
	}
	if len(stale) != 1 || stale[0] != "worker-A" {
		t.Fatalf("expected worker-A stale after 3 silent rounds, got %v", stale)
	}
}

func TestStalenessTrackerIgnoresNewMembers(t *testing.T) {
	ring := NewRing("tracker")
	ring.Join("worker-new")

	// Worker joined but never produced any BlockRef events.
	shared := New("tracker")

	tracker := NewStalenessTracker(1)

	// Even with threshold=1, a member with no events is never stale.
	for i := 0; i < 5; i++ {
		stale := tracker.Observe(ring, shared.Context())
		if len(stale) != 0 {
			t.Fatalf("round %d: new member with no events should not be stale, got %v", i, stale)
		}
	}
}

func TestStalenessTrackerReset(t *testing.T) {
	ring := NewRing("tracker")
	ring.Join("worker-A")

	w := NewWorker("worker-A")
	shared := New("tracker")
	for _, d := range w.BackupFile("file-1", []string{"H1"}) {
		shared.Merge(d)
	}

	tracker := NewStalenessTracker(2)
	tracker.Observe(ring, shared.Context())

	// 2 silent rounds → stale.
	tracker.Observe(ring, shared.Context())
	stale := tracker.Observe(ring, shared.Context())
	if !slices.Contains(stale, "worker-A") {
		t.Fatal("worker-A should be stale")
	}

	// Reset clears the state.
	tracker.Reset("worker-A")
	stale = tracker.Observe(ring, shared.Context())
	if slices.Contains(stale, "worker-A") {
		t.Fatal("worker-A should not be stale after reset")
	}
}
