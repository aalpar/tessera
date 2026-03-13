package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
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

	swept := w.Index().UnreferencedBlocks()
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
	swept := gcIndex.UnreferencedBlocks()
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

// TestEndToEndBackupDedupGC exercises the full pipeline:
//
//  1. Two workers share a BlockStore
//  2. Worker-1 backs up a file (Chunker → BlockStore → SnapshotRecipe → BlockRef)
//  3. Worker-2 backs up a similar file (expect dedup — shared blocks)
//  4. Sync deltas between workers
//  5. Delete one file, run GC after dominance check
//  6. Verify: surviving file reads back correctly, deleted file's unique blocks are swept
func TestEndToEndBackupDedupGC(t *testing.T) {
	ctx := context.Background()
	store, err := NewFSBlockStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chunker := NewChunker(DefaultChunkerConfig())

	// Create two workers sharing the same BlockStore.
	w1 := NewWorker("worker-1")
	w2 := NewWorker("worker-2")

	// Create test data: fileA and fileB share the first 32KB.
	fileA := make([]byte, 64*1024)
	rand.Read(fileA)
	fileB := make([]byte, 64*1024)
	copy(fileB, fileA[:32*1024])
	rand.Read(fileB[32*1024:])

	// Step 1: Worker-1 backs up fileA.
	recipeA, deltasA, err := WriteSnapshot(ctx, "file-A", bytes.NewReader(fileA), chunker, store, w1.Index())
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: Worker-2 backs up fileB.
	recipeB, deltasB, err := WriteSnapshot(ctx, "file-B", bytes.NewReader(fileB), chunker, store, w2.Index())
	if err != nil {
		t.Fatal(err)
	}

	// Verify dedup: some blocks should be shared.
	setA := make(map[string]bool)
	for _, b := range recipeA.Blocks {
		setA[b.Hash] = true
	}
	shared := 0
	for _, b := range recipeB.Blocks {
		if setA[b.Hash] {
			shared++
		}
	}
	if shared == 0 {
		t.Fatal("expected shared blocks between files with common prefix")
	}
	t.Logf("dedup: %d blocks shared between fileA (%d blocks) and fileB (%d blocks)",
		shared, len(recipeA.Blocks), len(recipeB.Blocks))

	// Step 3: Sync deltas between workers.
	for _, d := range deltasB {
		w1.Sync(d)
	}
	for _, d := range deltasA {
		w2.Sync(d)
	}

	// Step 4: Verify both files read back correctly.
	gotA, err := ReadSnapshot(ctx, recipeA, store)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotA, fileA) {
		t.Fatal("fileA round-trip mismatch")
	}

	gotB, err := ReadSnapshot(ctx, recipeB, store)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotB, fileB) {
		t.Fatal("fileB round-trip mismatch")
	}

	// Step 5: Delete fileA.
	// We need to remove all block refs and the recipe ref.
	allHashesA := make([]string, 0, len(recipeA.Blocks)+1)
	for _, b := range recipeA.Blocks {
		allHashesA = append(allHashesA, b.Hash)
	}
	allHashesA = append(allHashesA, recipeA.Version)
	deleteDeltas := w1.DeleteFile("file-A", allHashesA)
	w2.Sync(deleteDeltas...)

	// Step 6: GC.
	// Build GC context by merging both workers' contexts.
	gcIndex := New("gc")
	for _, d := range deltasA {
		gcIndex.Merge(d)
	}
	for _, d := range deltasB {
		gcIndex.Merge(d)
	}
	for _, d := range deleteDeltas {
		gcIndex.Merge(d)
	}

	if !Dominates(gcIndex.Context(), []*dotcontext.CausalContext{w1.Context(), w2.Context()}) {
		t.Fatal("GC should dominate after seeing all worker events")
	}

	swept, err := Sweep(ctx, gcIndex, store)
	if err != nil {
		t.Fatal(err)
	}

	if len(swept) == 0 {
		t.Fatal("expected unreferenced blocks to be swept after fileA deletion")
	}

	// Blocks unique to fileA should be swept.
	// Shared blocks should NOT be swept (still referenced by fileB).
	for _, h := range swept {
		if setA[h] {
			// This is a block that was in fileA. Check if it's also in fileB.
			for _, b := range recipeB.Blocks {
				if b.Hash == h {
					t.Fatalf("block %s is shared with fileB but was swept", h)
				}
			}
		}
	}

	// FileB should still read back correctly.
	gotB2, err := ReadSnapshot(ctx, recipeB, store)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotB2, fileB) {
		t.Fatal("fileB should still be readable after GC")
	}

	t.Logf("GC swept %d blocks after fileA deletion", len(swept))
}

// TestEndToEndAppendStream exercises two workers appending to the same
// logical stream and reading back in timestamp order.
func TestEndToEndAppendStream(t *testing.T) {
	ctx := context.Background()
	store, err := NewFSBlockStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chunker := NewChunker(DefaultChunkerConfig())

	r1 := NewAppendRecipe("w1")
	r2 := NewAppendRecipe("w2")
	index := New("shared")

	// Worker 1 appends a chunk at t=100.
	data1 := []byte("entry from worker 1")
	var chunks1 []Chunk
	for c := range chunker.Chunks(bytes.NewReader(data1)) {
		chunks1 = append(chunks1, c)
	}
	for _, c := range chunks1 {
		if err := store.Put(ctx, c.Hash, c.Data); err != nil {
			t.Fatal(err)
		}
		index.AddRef(c.Hash, "stream-1")
	}
	d1 := r1.Append(chunks1[0].Hash, 100)

	// Worker 2 appends a chunk at t=50 (earlier timestamp, but appended later).
	data2 := []byte("entry from worker 2")
	var chunks2 []Chunk
	for c := range chunker.Chunks(bytes.NewReader(data2)) {
		chunks2 = append(chunks2, c)
	}
	for _, c := range chunks2 {
		if err := store.Put(ctx, c.Hash, c.Data); err != nil {
			t.Fatal(err)
		}
		index.AddRef(c.Hash, "stream-1")
	}
	d2 := r2.Append(chunks2[0].Hash, 50)

	// Sync.
	r1.Merge(d2)
	r2.Merge(d1)

	// Both should read in timestamp order: w2's entry (t=50) before w1's (t=100).
	h1 := r1.Read()
	h2 := r2.Read()

	if len(h1) != 2 || len(h2) != 2 {
		t.Fatalf("expected 2 entries, got r1=%d r2=%d", len(h1), len(h2))
	}
	if h1[0] != chunks2[0].Hash || h1[1] != chunks1[0].Hash {
		t.Fatalf("r1: unexpected order: %v", h1)
	}
	assertSliceEqual(t, "append-stream", h1, h2)

	// Verify we can read back the actual data from the store.
	for _, hash := range h1 {
		_, err := store.Get(ctx, hash)
		if err != nil {
			t.Fatalf("block %s not found in store: %v", hash, err)
		}
	}
}
