package tessera

import (
	"crypto/sha256"
	"encoding/hex"
)

// Chunk is a content-addressed block of data produced by the Chunker.
type Chunk struct {
	Hash string // SHA-256 hex of Data
	Data []byte
}

// newChunk creates a Chunk from raw data, computing the content hash.
func newChunk(data []byte) Chunk {
	sum := sha256.Sum256(data)
	return Chunk{
		Hash: hex.EncodeToString(sum[:]),
		Data: append([]byte(nil), data...), // own copy
	}
}
