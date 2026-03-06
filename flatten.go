package tessera

import (
	"bytes"
	"context"
	"fmt"
)

// Flatten reads the full patched file, re-chunks it, and produces a clean
// recipe with no patches. Returns the new recipe and BlockRef deltas for
// the new chunks.
//
// The caller is responsible for:
//   - Removing flattened patches from the PatchIndex
//   - Removing old chunk references from BlockRef
func Flatten(
	ctx context.Context,
	fileID string,
	recipe *SnapshotRecipe,
	store BlockStore,
	index *BlockRef,
	patches *PatchIndex,
	chunker *Chunker,
) (*SnapshotRecipe, []*BlockRef, error) {
	totalSize := TotalSize(recipe.Blocks)
	data, err := PatchedReadRange(ctx, recipe, store, patches, fileID, 0, totalSize)
	if err != nil {
		return nil, nil, fmt.Errorf("flatten %s: read patched data: %w", fileID, err)
	}

	newRecipe, deltas, err := WriteSnapshot(ctx, fileID, bytes.NewReader(data), chunker, store, index)
	if err != nil {
		return nil, nil, fmt.Errorf("flatten %s: write snapshot: %w", fileID, err)
	}

	return newRecipe, deltas, nil
}
