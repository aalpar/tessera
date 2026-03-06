package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestCompactFileProducesSameBytes(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Write original file
	data := make([]byte, 30*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Apply patches
	patchData := []byte("PATCHED!!")
	_, err = WritePatch(ctx, "w1", "file-a", 5000, patchData, store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read patched file before compact
	patchedData, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 0, TotalSize(recipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}

	// Compact
	newRecipe, _, err := CompactFile(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Read from new recipe (no patches needed) should match
	flatData, err := ReadRange(ctx, newRecipe, store, 0, TotalSize(newRecipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(flatData, patchedData) {
		t.Fatal("compacted data doesn't match patched data")
	}
}

func TestCompactFileRemovesPatches(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 20*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	_, err = WritePatch(ctx, "w1", "file-a", 1000, []byte("XX"), store, patches)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WritePatch(ctx, "w1", "file-a", 2000, []byte("YY"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	if len(patches.Patches("file-a")) != 2 {
		t.Fatal("expected 2 patches before compact")
	}

	_, _, err = CompactFile(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Patches should be gone
	if len(patches.Patches("file-a")) != 0 {
		t.Fatalf("expected 0 patches after compact, got %d", len(patches.Patches("file-a")))
	}
}

func TestCompactFileOldBlocksUnreferenced(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 20*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Remember old block hashes
	oldHashes := make(map[string]bool)
	for _, b := range recipe.Blocks {
		oldHashes[b.Hash] = true
	}

	// Patch and compact
	_, err = WritePatch(ctx, "w1", "file-a", 1000, []byte("CHANGED"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	newRecipe, _, err := CompactFile(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// New blocks should be referenced
	for _, b := range newRecipe.Blocks {
		if !index.IsReferenced(b.Hash) {
			t.Errorf("new block %s should be referenced", b.Hash)
		}
	}

	// Old blocks that aren't in the new recipe should be unreferenced
	newHashes := make(map[string]bool)
	for _, b := range newRecipe.Blocks {
		newHashes[b.Hash] = true
	}
	for hash := range oldHashes {
		if !newHashes[hash] && index.IsReferenced(hash) {
			t.Errorf("old block %s not in new recipe but still referenced", hash)
		}
	}
}

func TestCompactFileConcurrentPatchSurvives(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches1 := NewPatchIndex("w1")
	patches2 := NewPatchIndex("w2")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Write original file
	data := make([]byte, 30*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Worker 1 adds P1
	d1, err := WritePatch(ctx, "w1", "file-a", 5000, []byte("PATCH_1"), store, patches1)
	if err != nil {
		t.Fatal(err)
	}

	// Sync P1 delta to worker 2 (both replicas see P1)
	patches2.Merge(d1)

	// Worker 1 compacts — flattens P1 into the recipe, removes P1 from patches1
	newRecipe, deltas, err := CompactFile(ctx, "file-a", recipe, store, index, patches1, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Worker 2 concurrently adds P2 (hasn't seen the compact yet)
	p2Data := []byte("CONCURRENT_PATCH")
	d2, err := WritePatch(ctx, "w2", "file-a", 10000, p2Data, store, patches2)
	if err != nil {
		t.Fatal(err)
	}

	// Sync: worker 1 gets P2, worker 2 gets compact's patch removal
	patches1.Merge(d2)
	if deltas.PatchRemove != nil {
		patches2.Merge(deltas.PatchRemove)
	}

	// Worker 1 should see P2 surviving (P1 was compacted away)
	p1Patches := patches1.Patches("file-a")
	if len(p1Patches) != 1 {
		t.Fatalf("w1: expected 1 surviving patch, got %d", len(p1Patches))
	}

	// Worker 2 should also see only P2 (add-wins: P2 survives removal of P1)
	p2Patches := patches2.Patches("file-a")
	if len(p2Patches) != 1 {
		t.Fatalf("w2: expected 1 surviving patch, got %d", len(p2Patches))
	}

	// P2 should apply correctly over the new (compacted) recipe
	got, err := PatchedReadRange(ctx, newRecipe, store, patches1, "file-a", 10000, uint64(len(p2Data)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, p2Data) {
		t.Fatal("concurrent patch should apply correctly over compacted recipe")
	}
}

func TestCompactFileNoPatches(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 20*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Compact with no patches — should still work
	newRecipe, _, err := CompactFile(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Data should be identical
	orig, _ := ReadRange(ctx, recipe, store, 0, TotalSize(recipe.Blocks))
	compacted, _ := ReadRange(ctx, newRecipe, store, 0, TotalSize(newRecipe.Blocks))
	if !bytes.Equal(orig, compacted) {
		t.Fatal("compact with no patches should produce identical data")
	}
}
