package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestSnapshotRecipeRoundTrip(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("worker-1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Create test data.
	data := make([]byte, 50*1024)
	rand.Read(data)

	// Write.
	recipe, deltas, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipe.Blocks) == 0 {
		t.Fatal("recipe has no blocks")
	}
	if len(deltas) == 0 {
		t.Fatal("no deltas produced")
	}

	// Read back.
	got, err := ReadSnapshot(ctx, recipe, store)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("round-trip data mismatch")
	}
}

func TestSnapshotRecipeDedup(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("worker-1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Two copies of the same data.
	data := make([]byte, 40*1024)
	rand.Read(data)

	r1, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}
	r2, _, err := WriteSnapshot(ctx, "file-b", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Same data → same blocks → same recipe version.
	if r1.Version != r2.Version {
		t.Fatal("identical data should produce identical recipe versions")
	}

	// Both files reference the same blocks.
	for _, hash := range r1.Blocks {
		refs := index.Refs(hash)
		hasA, hasB := false, false
		for _, r := range refs {
			if r == "file-a" {
				hasA = true
			}
			if r == "file-b" {
				hasB = true
			}
		}
		if !hasA || !hasB {
			t.Fatalf("block %s: expected refs from both files, got %v", hash, refs)
		}
	}
}

func TestSnapshotRecipeSerialization(t *testing.T) {
	blocks := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	r := NewSnapshotRecipe("test-file", blocks)

	data := r.Serialize()
	r2 := DeserializeSnapshotRecipe("test-file", data)

	if r.Version != r2.Version {
		t.Fatalf("version mismatch: %s vs %s", r.Version, r2.Version)
	}
	if len(r2.Blocks) != len(blocks) {
		t.Fatalf("block count mismatch: %d vs %d", len(r2.Blocks), len(blocks))
	}
	for i := range blocks {
		if r2.Blocks[i] != blocks[i] {
			t.Fatalf("block %d mismatch", i)
		}
	}
}

func TestSnapshotRecipePartialDedup(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("worker-1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// File A: 64KB of random data.
	dataA := make([]byte, 64*1024)
	rand.Read(dataA)

	// File B: same first 32KB, different second half.
	dataB := make([]byte, 64*1024)
	copy(dataB, dataA[:32*1024])
	rand.Read(dataB[32*1024:])

	r1, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(dataA), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}
	r2, _, err := WriteSnapshot(ctx, "file-b", bytes.NewReader(dataB), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Count shared blocks.
	set1 := make(map[string]bool)
	for _, h := range r1.Blocks {
		set1[h] = true
	}
	shared := 0
	for _, h := range r2.Blocks {
		if set1[h] {
			shared++
		}
	}

	if shared == 0 {
		t.Fatal("expected some shared blocks between files with common prefix")
	}
	t.Logf("partial dedup: %d/%d blocks shared", shared, len(r1.Blocks))
}
