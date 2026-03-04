package tessera

import (
	"slices"
	"testing"

	"github.com/aalpar/crdt/dotcontext"
)

// TestBasicDedup verifies that two workers backing up different files
// that share blocks correctly track all references after sync.
func TestBasicDedup(t *testing.T) {
	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	// Worker 1 backs up file-A referencing blocks H1, H2, H3.
	deltasA := w1.BackupFile("file-A", []string{"H1", "H2", "H3"})

	// Worker 2 backs up file-B referencing blocks H2, H3, H4.
	deltasB := w2.BackupFile("file-B", []string{"H2", "H3", "H4"})

	// Sync: each worker receives the other's deltas.
	w1.Sync(deltasB...)
	w2.Sync(deltasA...)

	// Both workers should see all references.
	for _, w := range []*Worker{w1, w2} {
		idx := w.Index()

		// H1 referenced only by file-A.
		assertRefs(t, idx, "H1", []string{"file-A"})

		// H2 referenced by both files.
		assertRefs(t, idx, "H2", []string{"file-A", "file-B"})

		// H3 referenced by both files.
		assertRefs(t, idx, "H3", []string{"file-A", "file-B"})

		// H4 referenced only by file-B.
		assertRefs(t, idx, "H4", []string{"file-B"})

		// All blocks are referenced.
		if unreferenced := idx.UnreferencedBlocks(); len(unreferenced) != 0 {
			t.Errorf("expected no unreferenced blocks, got %v", unreferenced)
		}
	}
}

// TestFileDeletionAndGC verifies that after a worker deletes a file
// and syncs, GC can sweep unreferenced blocks.
func TestFileDeletionAndGC(t *testing.T) {
	w := NewWorker("worker-1")

	// Back up file-A referencing H1, H2; file-B referencing H2, H3.
	w.BackupFile("file-A", []string{"H1", "H2"})
	w.BackupFile("file-B", []string{"H2", "H3"})

	// Delete file-A's references.
	w.DeleteFile("file-A", []string{"H1", "H2"})

	// H1 should be unreferenced (only file-A had it).
	if w.Index().IsReferenced("H1") {
		t.Error("H1 should be unreferenced after file-A deletion")
	}

	// H2 should still be referenced (file-B has it).
	if !w.Index().IsReferenced("H2") {
		t.Error("H2 should still be referenced by file-B")
	}

	// H3 still referenced by file-B.
	if !w.Index().IsReferenced("H3") {
		t.Error("H3 should still be referenced by file-B")
	}

	// GC: single worker, trivially dominant.
	gcCtx := w.Context().Clone()
	if !Dominates(gcCtx, []*dotcontext.CausalContext{w.Context()}) {
		t.Error("GC should dominate when it has the worker's full context")
	}

	swept := Sweep(w.Index())
	if !slices.Contains(swept, "H1") {
		t.Errorf("expected H1 in swept blocks, got %v", swept)
	}
	for _, h := range swept {
		if h == "H2" || h == "H3" {
			t.Errorf("block %s should not be swept (still referenced)", h)
		}
	}
}

// TestAddWinsSafety verifies that concurrent add and remove of the
// same block reference resolves in favor of add.
func TestAddWinsSafety(t *testing.T) {
	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	// Worker 1 backs up file-A referencing H1.
	deltasSetup := w1.BackupFile("file-A", []string{"H1"})

	// Sync setup to worker 2.
	w2.Sync(deltasSetup...)

	// --- Concurrent operations (no sync between them) ---

	// Worker 1 removes file-A's reference to H1.
	deltasRemove := w1.DeleteFile("file-A", []string{"H1"})

	// Worker 2 (concurrently) adds file-B's reference to H1.
	deltasAdd := w2.BackupFile("file-B", []string{"H1"})

	// --- Sync both ways ---
	w1.Sync(deltasAdd...)
	w2.Sync(deltasRemove...)

	// H1 must survive: file-B's add wins over file-A's remove.
	if !w1.Index().IsReferenced("H1") {
		t.Error("worker-1: H1 should survive (add-wins)")
	}
	if !w2.Index().IsReferenced("H1") {
		t.Error("worker-2: H1 should survive (add-wins)")
	}

	// file-A's reference should be gone; file-B's should remain.
	assertRefs(t, w1.Index(), "H1", []string{"file-B"})
	assertRefs(t, w2.Index(), "H1", []string{"file-B"})
}

// TestGCDominanceGate verifies that GC cannot sweep until it has seen
// all worker events.
func TestGCDominanceGate(t *testing.T) {
	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	// Worker 1 backs up file-A referencing H1.
	deltas1 := w1.BackupFile("file-A", []string{"H1"})

	// Worker 2 backs up file-B referencing H2.
	deltas2 := w2.BackupFile("file-B", []string{"H2"})

	// GC starts with only worker-1's context.
	gcIndex := New("gc")
	gcIndex.Merge(deltas1[0])

	// GC does NOT dominate: it hasn't seen worker-2's events.
	if Dominates(gcIndex.Context(), []*dotcontext.CausalContext{w1.Context(), w2.Context()}) {
		t.Error("GC should NOT dominate before seeing worker-2's events")
	}

	// Sync worker-2's deltas into GC.
	gcIndex.Merge(deltas2[0])

	// Now GC dominates both workers.
	if !Dominates(gcIndex.Context(), []*dotcontext.CausalContext{w1.Context(), w2.Context()}) {
		t.Error("GC should dominate after seeing all worker events")
	}

	// All blocks are referenced, so sweep returns nothing.
	swept := Sweep(gcIndex)
	if len(swept) != 0 {
		t.Errorf("expected no swept blocks (all referenced), got %v", swept)
	}
}

// assertRefs checks that the file IDs referencing contentHash match
// the expected set (order-independent).
func assertRefs(t *testing.T, idx *BlockRef, contentHash string, expected []string) {
	t.Helper()
	got := idx.Refs(contentHash)
	slices.Sort(got)
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Errorf("Refs(%q) = %v, want %v", contentHash, got, expected)
	}
}
