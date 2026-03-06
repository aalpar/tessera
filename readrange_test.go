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
