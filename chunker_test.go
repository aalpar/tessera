package tessera

import (
	"bytes"
	"crypto/rand"
	"io"
	"strings"
	"testing"
)

func TestChunkerDeterministic(t *testing.T) {
	data := make([]byte, 64*1024)
	rand.Read(data)

	cfg := DefaultChunkerConfig()
	c := NewChunker(cfg)

	var hashes1, hashes2 []string
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		hashes1 = append(hashes1, chunk.Hash)
	}
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		hashes2 = append(hashes2, chunk.Hash)
	}

	if len(hashes1) != len(hashes2) {
		t.Fatalf("chunk count differs: %d vs %d", len(hashes1), len(hashes2))
	}
	for i := range hashes1 {
		if hashes1[i] != hashes2[i] {
			t.Fatalf("chunk %d hash differs", i)
		}
	}
}

func TestChunkerBlockSizeBounds(t *testing.T) {
	data := make([]byte, 128*1024)
	rand.Read(data)

	cfg := DefaultChunkerConfig()
	c := NewChunker(cfg)

	count := 0
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		count++
		if len(chunk.Data) > cfg.MaxSize {
			t.Fatalf("chunk size %d exceeds max %d", len(chunk.Data), cfg.MaxSize)
		}
	}

	if count < 2 {
		t.Fatalf("expected multiple chunks from %d bytes, got %d", len(data), count)
	}

	// All chunks except the last must be >= MinSize.
	i := 0
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		i++
		if i < count && len(chunk.Data) < cfg.MinSize {
			t.Fatalf("non-final chunk %d size %d below min %d", i, len(chunk.Data), cfg.MinSize)
		}
	}
}

func TestChunkerContentShiftStability(t *testing.T) {
	// Create original data.
	original := make([]byte, 64*1024)
	rand.Read(original)

	// Insert 100 bytes near the beginning (after 1KB).
	insertion := make([]byte, 100)
	rand.Read(insertion)
	modified := make([]byte, 0, len(original)+len(insertion))
	modified = append(modified, original[:1024]...)
	modified = append(modified, insertion...)
	modified = append(modified, original[1024:]...)

	cfg := DefaultChunkerConfig()
	c := NewChunker(cfg)

	origChunks := collectChunks(c, original)
	modChunks := collectChunks(c, modified)

	// Count how many chunks from the original appear in the modified version.
	origSet := make(map[string]bool)
	for _, ch := range origChunks {
		origSet[ch.Hash] = true
	}
	shared := 0
	for _, ch := range modChunks {
		if origSet[ch.Hash] {
			shared++
		}
	}

	// With content-defined chunking, most chunks after the insertion point
	// should be identical. We expect at least 50% chunk reuse.
	minShared := len(origChunks) / 2
	if shared < minShared {
		t.Fatalf("content-shift stability: only %d/%d chunks shared (want >= %d)",
			shared, len(origChunks), minShared)
	}
}

func TestChunkerEmptyInput(t *testing.T) {
	c := NewChunker(DefaultChunkerConfig())
	count := 0
	for range c.Chunks(bytes.NewReader(nil)) {
		count++
	}
	if count != 0 {
		t.Fatalf("expected 0 chunks from empty input, got %d", count)
	}
}

func TestChunkerSmallInput(t *testing.T) {
	c := NewChunker(DefaultChunkerConfig())
	data := []byte("small")
	var chunks []Chunk
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk from small input, got %d", len(chunks))
	}
	if !bytes.Equal(chunks[0].Data, data) {
		t.Fatalf("chunk data mismatch")
	}
}

func TestChunkerRoundTrip(t *testing.T) {
	data := make([]byte, 100*1024)
	rand.Read(data)

	c := NewChunker(DefaultChunkerConfig())
	var reassembled []byte
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		reassembled = append(reassembled, chunk.Data...)
	}
	if !bytes.Equal(reassembled, data) {
		t.Fatal("reassembled data does not match original")
	}
}

func TestChunkerRepeatedContent(t *testing.T) {
	// Repeating pattern should produce identical chunks (dedup opportunity).
	block := make([]byte, 16*1024)
	rand.Read(block)

	// Repeat the block 4 times.
	data := bytes.Repeat(block, 4)

	c := NewChunker(DefaultChunkerConfig())
	chunks := collectChunks(c, data)

	hashCount := make(map[string]int)
	for _, ch := range chunks {
		hashCount[ch.Hash]++
	}

	// With repeated content, we expect some hash reuse.
	hasReuse := false
	for _, count := range hashCount {
		if count > 1 {
			hasReuse = true
			break
		}
	}
	if !hasReuse {
		t.Log("warning: no chunk hash reuse in repeated content (not fatal, but unexpected)")
	}
}

func TestChunkHash(t *testing.T) {
	data := []byte("hello world")
	c := newChunk(data)
	if c.Hash == "" {
		t.Fatal("hash should not be empty")
	}
	if len(c.Hash) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars", len(c.Hash))
	}
	// Verify it's valid hex.
	if strings.ContainsFunc(c.Hash, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'))
	}) {
		t.Fatalf("hash contains non-hex characters: %s", c.Hash)
	}
}

func collectChunks(c *Chunker, data []byte) []Chunk {
	var chunks []Chunk
	for chunk := range c.Chunks(bytes.NewReader(data)) {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// Verify the chunker works with a slow reader (1 byte at a time).
func TestChunkerSlowReader(t *testing.T) {
	data := make([]byte, 32*1024)
	rand.Read(data)

	c := NewChunker(DefaultChunkerConfig())

	// Collect chunks from a normal reader.
	normal := collectChunks(c, data)

	// Collect chunks from a 1-byte-at-a-time reader.
	var slow []Chunk
	for chunk := range c.Chunks(&slowReader{data: data}) {
		slow = append(slow, chunk)
	}

	if len(normal) != len(slow) {
		t.Fatalf("chunk count differs: normal=%d slow=%d", len(normal), len(slow))
	}
	for i := range normal {
		if normal[i].Hash != slow[i].Hash {
			t.Fatalf("chunk %d hash differs between normal and slow reader", i)
		}
	}
}

type slowReader struct {
	data []byte
	pos  int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	if r.pos >= len(r.data) {
		return 1, io.EOF
	}
	return 1, nil
}
