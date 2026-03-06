package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestFlattenProducesSameBytes(t *testing.T) {
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

	// Apply a patch
	patchData := []byte("PATCHED DATA HERE!!")
	_, err = WritePatch(ctx, "w1", "file-a", 5000, patchData, store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read patched file
	patchedData, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 0, TotalSize(recipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}

	// Flatten
	newRecipe, _, err := Flatten(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Read from new recipe (no patches) should equal patched read
	flatData, err := ReadRange(ctx, newRecipe, store, 0, TotalSize(newRecipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(flatData, patchedData) {
		t.Fatal("flattened data doesn't match patched data")
	}
}

func TestFlattenNewRecipeHasSizes(t *testing.T) {
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

	newRecipe, _, err := Flatten(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Every block should have a non-zero size
	for i, b := range newRecipe.Blocks {
		if b.Size == 0 {
			t.Errorf("block %d has zero size", i)
		}
	}

	// Total size should match original
	if TotalSize(newRecipe.Blocks) != TotalSize(recipe.Blocks) {
		t.Errorf("total size changed: %d vs %d", TotalSize(newRecipe.Blocks), TotalSize(recipe.Blocks))
	}
}
