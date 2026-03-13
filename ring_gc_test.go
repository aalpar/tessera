package tessera

import (
	"testing"

	"github.com/aalpar/crdt/dotcontext"
)

// TestGCWithRingMembership exercises the full ring-aware GC flow:
// join → backup → sync → delete → dominance check via ring → sweep.
//
// GC runs from worker-1's perspective: its local index retains empty
// entries after delete (needed by UnreferencedBlocks). The ring
// determines which member contexts to check for dominance.
func TestGCWithRingMembership(t *testing.T) {
	ring1 := NewRing("node-1")
	ring2 := NewRing("node-2")

	d1 := ring1.Join("worker-1")
	d2 := ring2.Join("worker-2")
	ring1.Merge(d2)
	ring2.Merge(d1)

	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	deltasA := w1.BackupFile("file-A", []string{"H1"})
	deltasB := w2.BackupFile("file-B", []string{"H2"})

	// Sync BlockRef deltas.
	w1.Sync(deltasB...)
	w2.Sync(deltasA...)

	// Worker-1 deletes file-A locally, then syncs the delete to worker-2.
	deleteDeltas := w1.DeleteFile("file-A", []string{"H1"})
	w2.Sync(deleteDeltas...)

	// GC from worker-1's perspective.
	memberCtxs := map[string]*dotcontext.CausalContext{
		"worker-1": w1.Context(),
		"worker-2": w2.Context(),
	}

	if !DominatesRing(w1.Context(), ring1, memberCtxs) {
		t.Fatal("worker-1 should dominate all ring members after full sync")
	}

	// Sweep from worker-1's local index (which retains the empty H1 entry).
	swept := w1.Index().UnreferencedBlocks()
	if len(swept) != 1 || swept[0] != "H1" {
		t.Fatalf("expected [H1] swept, got %v", swept)
	}
}

// TestGCBlockedByUnknownMember verifies that GC cannot proceed
// when the ring contains a member whose context is unknown.
func TestGCBlockedByUnknownMember(t *testing.T) {
	ring := NewRing("node-1")
	ring.Join("worker-1")
	ring.Join("worker-2")

	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	// Both workers produce events.
	deltasA := w1.BackupFile("file-A", []string{"H1"})
	deltasB := w2.BackupFile("file-B", []string{"H2"})

	// GC only knows about worker-1.
	gcIndex := New("gc")
	for _, d := range deltasA {
		gcIndex.Merge(d)
	}
	// Also merge worker-2's deltas into GC index so it has seen the events,
	// but don't provide worker-2's context in the map.
	for _, d := range deltasB {
		gcIndex.Merge(d)
	}

	memberCtxs := map[string]*dotcontext.CausalContext{
		"worker-1": w1.Context(),
		// worker-2 missing — context unknown.
	}

	// GC knows worker-2 exists (has events from it) but doesn't have
	// its latest context. Should fail dominance.
	if DominatesRing(gcIndex.Context(), ring, memberCtxs) {
		t.Fatal("GC should NOT dominate when a member's context is unknown")
	}
}

// TestGCAfterEviction verifies that evicting a dead worker from the
// ring allows GC to proceed without waiting for that worker.
func TestGCAfterEviction(t *testing.T) {
	ring := NewRing("node-1")
	ring.Join("worker-1")
	ring.Join("worker-2")

	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	deltasA := w1.BackupFile("file-A", []string{"H1"})
	deltasB := w2.BackupFile("file-B", []string{"H2"})

	// Worker-1 syncs worker-2's data.
	w1.Sync(deltasB...)

	// Worker-1 deletes file-A locally.
	w1.DeleteFile("file-A", []string{"H1"})

	// Worker-2 is dead — context unavailable.
	memberCtxs := map[string]*dotcontext.CausalContext{
		"worker-1": w1.Context(),
	}

	// Can't GC: worker-2 still in ring and has events.
	if DominatesRing(w1.Context(), ring, memberCtxs) {
		t.Fatal("should not dominate with dead worker still in ring")
	}

	// Evict worker-2 from ring.
	ring.Remove("worker-2")

	// Now GC dominates — worker-2 is no longer a member.
	if !DominatesRing(w1.Context(), ring, memberCtxs) {
		t.Fatal("should dominate after evicting dead worker")
	}

	// Sweep from worker-1's local index.
	swept := w1.Index().UnreferencedBlocks()
	if len(swept) != 1 || swept[0] != "H1" {
		t.Fatalf("expected [H1] swept after eviction, got %v", swept)
	}

	_ = deltasA // deltasA used to set up w1's state
}

// TestStalenessToEvictionFlow exercises the full pipeline:
// active worker → staleness detected → eviction → GC unblocked.
func TestStalenessToEvictionFlow(t *testing.T) {
	ring := NewRing("node-1")
	ring.Join("worker-1")
	ring.Join("worker-2")

	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	// Both produce initial events.
	deltasA := w1.BackupFile("file-A", []string{"H1"})
	deltasB := w2.BackupFile("file-B", []string{"H2"})

	shared := New("observer")
	for _, d := range deltasA {
		shared.Merge(d)
	}
	for _, d := range deltasB {
		shared.Merge(d)
	}

	tracker := NewStalenessTracker(3)
	tracker.Observe(ring, shared.Context())

	// Worker-2 goes silent. Worker-1 keeps producing.
	for range 3 {
		for _, d := range w1.BackupFile("file-A", []string{"H3"}) {
			shared.Merge(d)
		}
		tracker.Observe(ring, shared.Context())
	}

	stale := tracker.Observe(ring, shared.Context())
	found := false
	for _, s := range stale {
		if s == "worker-2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected worker-2 to be stale, got %v", stale)
	}

	// Evict stale worker.
	ring.Remove("worker-2")
	tracker.Reset("worker-2")

	// GC can now proceed without worker-2.
	memberCtxs := map[string]*dotcontext.CausalContext{
		"worker-1": w1.Context(),
	}
	if !DominatesRing(shared.Context(), ring, memberCtxs) {
		t.Fatal("should dominate after evicting stale worker")
	}
}
