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

// PatchedReadRange reads a byte range from a file, applying any patches
// from the PatchIndex in timestamp order (LWW semantics).
func PatchedReadRange(ctx context.Context, recipe *SnapshotRecipe, store BlockStore, patches *PatchIndex, fileID string, offset, length uint64) ([]byte, error) {
	result, err := ReadRange(ctx, recipe, store, offset, length)
	if err != nil {
		return nil, err
	}

	for _, patch := range patches.Patches(fileID) {
		if err := applyPatch(ctx, result, offset, length, patch, store); err != nil {
			return nil, fmt.Errorf("patched read %s: %w", fileID, err)
		}
	}

	return result, nil
}

func applyPatch(ctx context.Context, buf []byte, rangeOffset, rangeLength uint64, patch patchEntry, store BlockStore) error {
	patchEnd := patch.Offset + patch.Size
	rangeEnd := rangeOffset + rangeLength

	overlapStart := max(patch.Offset, rangeOffset)
	overlapEnd := min(patchEnd, rangeEnd)
	if overlapStart >= overlapEnd {
		return nil
	}

	patchData, err := store.Get(ctx, patch.DataHash)
	if err != nil {
		return fmt.Errorf("patch %s offset %d: %w", patch.DataHash, patch.Offset, err)
	}

	srcOffset := overlapStart - patch.Offset
	dstOffset := overlapStart - rangeOffset
	copyLen := overlapEnd - overlapStart
	copy(buf[dstOffset:dstOffset+copyLen], patchData[srcOffset:srcOffset+copyLen])
	return nil
}
