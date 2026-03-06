package tessera

import "testing"

func TestChunkForOffset(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
		{Hash: "bbb", Size: 200},
		{Hash: "ccc", Size: 150},
	}

	tests := []struct {
		offset    uint64
		wantIndex int
		wantInner uint64
	}{
		{0, 0, 0},       // start of first chunk
		{99, 0, 99},     // end of first chunk
		{100, 1, 0},     // start of second chunk
		{299, 1, 199},   // end of second chunk
		{300, 2, 0},     // start of third chunk
		{449, 2, 149},   // end of third chunk
	}

	for _, tt := range tests {
		idx, inner, err := ChunkForOffset(blocks, tt.offset)
		if err != nil {
			t.Errorf("offset %d: unexpected error: %v", tt.offset, err)
			continue
		}
		if idx != tt.wantIndex {
			t.Errorf("offset %d: chunk index = %d, want %d", tt.offset, idx, tt.wantIndex)
		}
		if inner != tt.wantInner {
			t.Errorf("offset %d: inner offset = %d, want %d", tt.offset, inner, tt.wantInner)
		}
	}
}

func TestChunkForOffsetOutOfRange(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
	}
	_, _, err := ChunkForOffset(blocks, 100)
	if err == nil {
		t.Error("expected error for offset beyond file end")
	}
}

func TestChunksForRange(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
		{Hash: "bbb", Size: 200},
		{Hash: "ccc", Size: 150},
	}

	// Range spanning chunks 0 and 1
	start, end, err := ChunksForRange(blocks, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if start != 0 || end != 2 {
		t.Errorf("got [%d,%d), want [0,2)", start, end)
	}

	// Range within single chunk
	start, end, err = ChunksForRange(blocks, 110, 50)
	if err != nil {
		t.Fatal(err)
	}
	if start != 1 || end != 2 {
		t.Errorf("got [%d,%d), want [1,2)", start, end)
	}

	// Range spanning all chunks
	start, end, err = ChunksForRange(blocks, 0, 450)
	if err != nil {
		t.Fatal(err)
	}
	if start != 0 || end != 3 {
		t.Errorf("got [%d,%d), want [0,3)", start, end)
	}
}

func TestTotalSize(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
		{Hash: "bbb", Size: 200},
	}
	if got := TotalSize(blocks); got != 300 {
		t.Errorf("got %d, want 300", got)
	}
}
