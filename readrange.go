package tessera

import (
	"context"
	"fmt"
)

// ReadRange reads the byte range [offset, offset+length) from a file
// described by the given recipe, fetching chunks from the store.
func ReadRange(ctx context.Context, recipe *SnapshotRecipe, store BlockStore, offset, length uint64) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}

	startChunk, endChunk, err := ChunksForRange(recipe.Blocks, offset, length)
	if err != nil {
		return nil, fmt.Errorf("read range %s: %w", recipe.FileID, err)
	}

	result := make([]byte, length)
	var chunkStart uint64
	for i := range startChunk {
		chunkStart += recipe.Blocks[i].Size
	}

	for i := startChunk; i < endChunk; i++ {
		block := recipe.Blocks[i]
		data, err := store.Get(ctx, block.Hash)
		if err != nil {
			return nil, fmt.Errorf("read range %s: block %d (%s): %w", recipe.FileID, i, block.Hash, err)
		}

		// Compute overlap between this chunk and the requested range.
		chunkEnd := chunkStart + block.Size

		copyStart := max(offset, chunkStart)
		copyEnd := min(offset+length, chunkEnd)

		srcOffset := copyStart - chunkStart
		dstOffset := copyStart - offset
		copy(result[dstOffset:], data[srcOffset:srcOffset+(copyEnd-copyStart)])

		chunkStart = chunkEnd
	}

	return result, nil
}
