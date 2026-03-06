package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestReadRangeFullFile(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 50*1024)
	rand.Read(data)

	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Read entire file via ReadRange
	got, err := ReadRange(ctx, recipe, store, 0, TotalSize(recipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("full file ReadRange mismatch")
	}
}

func TestReadRangePartial(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 50*1024)
	rand.Read(data)

	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Read bytes 1000-2000
	got, err := ReadRange(ctx, recipe, store, 1000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[1000:2000]) {
		t.Fatal("partial ReadRange mismatch")
	}
}

func TestPatchedReadRange(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Write a file
	data := make([]byte, 20*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Write a patch at offset 1000: overwrite 10 bytes
	patchData := []byte("XXXXXXXXXX")
	_, err = WritePatch(ctx, "w1", "file-a", 1000, patchData, store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read patched region
	got, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 995, 20)
	if err != nil {
		t.Fatal(err)
	}

	// First 5 bytes should be original data
	if !bytes.Equal(got[:5], data[995:1000]) {
		t.Error("pre-patch bytes mismatch")
	}
	// Next 10 bytes should be patch data
	if !bytes.Equal(got[5:15], patchData) {
		t.Error("patched bytes mismatch")
	}
	// Last 5 bytes should be original data
	if !bytes.Equal(got[15:], data[1010:1015]) {
		t.Error("post-patch bytes mismatch")
	}
}

func TestPatchedReadRangeOverlapping(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 10*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Earlier patch: write "AAAA" at offset 100
	_, err = WritePatch(ctx, "w1", "file-a", 100, []byte("AAAA"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Later patch: write "BB" at offset 102 (overlaps with first patch)
	_, err = WritePatch(ctx, "w1", "file-a", 102, []byte("BB"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read the patched region
	got, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 100, 4)
	if err != nil {
		t.Fatal(err)
	}

	// Expected: "AA" from first patch, then "BB" from second (overwrites positions 102-103)
	if string(got) != "AABB" {
		t.Errorf("expected AABB, got %q", string(got))
	}
}

func TestPatchedReadRangeNoPatches(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 10*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Read without any patches — should match original
	got, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 500, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[500:600]) {
		t.Fatal("unpatched read mismatch")
	}
}

func TestReadRangeSpansChunks(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	// Small chunks to force multiple chunks in a small file
	chunker := NewChunker(ChunkerConfig{MinSize: 256, MaxSize: 1024, TargetSize: 512})
	ctx := context.Background()

	data := make([]byte, 8*1024)
	rand.Read(data)

	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	if len(recipe.Blocks) < 3 {
		t.Skipf("need at least 3 chunks, got %d", len(recipe.Blocks))
	}

	// Read a range that spans the first two chunks
	chunkOneSize := recipe.Blocks[0].Size
	offset := chunkOneSize - 10 // 10 bytes before chunk boundary
	length := uint64(30)        // spans into second chunk

	got, err := ReadRange(ctx, recipe, store, offset, length)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[offset:offset+length]) {
		t.Fatal("cross-chunk ReadRange mismatch")
	}
}
