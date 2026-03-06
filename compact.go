package tessera

import (
	"context"
	"fmt"
)

// CompactDeltas groups the deltas produced by CompactFile for replication.
type CompactDeltas struct {
	BlockRefAdds    []*BlockRef // new chunks registered
	BlockRefRemoves []*BlockRef // old chunks deregistered
	PatchRemove     *PatchIndex // flattened patches removed
}

// CompactFile flattens a patched file into a clean recipe, removes the
// flattened patches from the PatchIndex, and deregisters old block refs.
//
// No dominance check is needed — the CRDT algebra guarantees that
// concurrent patch adds from other replicas survive the removal.
func CompactFile(
	ctx context.Context,
	fileID string,
	recipe *SnapshotRecipe,
	store BlockStore,
	index *BlockRef,
	patches *PatchIndex,
	chunker *Chunker,
) (*SnapshotRecipe, *CompactDeltas, error) {
	// 1. Snapshot patches before flatten
	currentPatches := patches.Patches(fileID)

	// 2. Flatten: read patched data, re-chunk, store new blocks
	newRecipe, addDeltas, err := Flatten(ctx, fileID, recipe, store, index, patches, chunker)
	if err != nil {
		return nil, nil, fmt.Errorf("compact %s: %w", fileID, err)
	}

	// 3. Remove flattened patches from PatchIndex
	var patchDelta *PatchIndex
	if len(currentPatches) > 0 {
		patchDelta = patches.RemovePatches(fileID, currentPatches)
	}

	// 4. Remove old block refs for chunks no longer in the new recipe.
	// Blocks shared between old and new recipes must keep their refs.
	newHashes := make(map[string]bool, len(newRecipe.Blocks)+1)
	for _, b := range newRecipe.Blocks {
		newHashes[b.Hash] = true
	}
	newHashes[newRecipe.Version] = true

	var staleHashes []string
	for _, b := range recipe.Blocks {
		if !newHashes[b.Hash] {
			staleHashes = append(staleHashes, b.Hash)
		}
	}
	if !newHashes[recipe.Version] {
		staleHashes = append(staleHashes, recipe.Version)
	}
	removeDeltas := index.RemoveFileRefs(fileID, staleHashes)

	return newRecipe, &CompactDeltas{
		BlockRefAdds:    addDeltas,
		BlockRefRemoves: removeDeltas,
		PatchRemove:     patchDelta,
	}, nil
}
