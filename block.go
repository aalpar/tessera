package tessera

import "fmt"

// Block is a content-addressed chunk with its size, used in recipes
// for random-access offset computation.
type Block struct {
	Hash string
	Size uint64
}

// TotalSize returns the sum of all block sizes.
func TotalSize(blocks []Block) uint64 {
	var total uint64
	for _, b := range blocks {
		total += b.Size
	}
	return total
}

// ChunkForOffset returns the chunk index and intra-chunk offset for
// the given byte offset within a file described by blocks.
func ChunkForOffset(blocks []Block, offset uint64) (index int, inner uint64, err error) {
	var cumulative uint64
	for i, b := range blocks {
		if offset < cumulative+b.Size {
			return i, offset - cumulative, nil
		}
		cumulative += b.Size
	}
	return 0, 0, fmt.Errorf("offset %d beyond file size %d", offset, cumulative)
}

// ChunksForRange returns the half-open range [start, end) of chunk indices
// that overlap the byte range [offset, offset+length).
func ChunksForRange(blocks []Block, offset, length uint64) (start, end int, err error) {
	if length == 0 {
		return 0, 0, nil
	}
	startIdx, _, err := ChunkForOffset(blocks, offset)
	if err != nil {
		return 0, 0, err
	}
	lastByte := offset + length - 1
	endIdx, _, err := ChunkForOffset(blocks, lastByte)
	if err != nil {
		return 0, 0, err
	}
	return startIdx, endIdx + 1, nil
}
