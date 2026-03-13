package tessera

import (
	"context"
	"fmt"

	"github.com/aalpar/crdt/dotcontext"
)

// Dominates reports whether gcCtx has observed all events from every
// worker context. GC may only sweep unreferenced blocks after
// establishing dominance — otherwise it could delete a block that
// a worker has concurrently re-referenced.
func Dominates(gcCtx *dotcontext.CausalContext, workerContexts []*dotcontext.CausalContext) bool {
	for _, wCtx := range workerContexts {
		for _, id := range wCtx.ReplicaIDs() {
			if gcCtx.Max(id) < wCtx.Max(id) {
				return false
			}
		}
	}
	return true
}

// DominatesRing reports whether gcCtx has observed all events from
// every current ring member. memberCtxs maps worker IDs to their
// last-known causal contexts (collected during delta exchange).
// Members not present in memberCtxs are treated as unobserved —
// dominance fails unless the member has no events (max seq == 0).
func DominatesRing(gcCtx *dotcontext.CausalContext, ring *Ring, memberCtxs map[string]*dotcontext.CausalContext) bool {
	for _, member := range ring.Members() {
		wCtx, ok := memberCtxs[member]
		if !ok {
			// Unknown member — check if they have any events in gcCtx.
			// If gcCtx has no record of this replica, they might have
			// events we haven't seen. Fail safe.
			if gcCtx.Max(dotcontext.ReplicaID(member)) > 0 {
				// We know this replica exists but don't have their
				// latest context — can't prove dominance.
				return false
			}
			// max==0 means no events from this replica anywhere.
			// An idle member can't block GC.
			continue
		}
		for _, id := range wCtx.ReplicaIDs() {
			if gcCtx.Max(id) < wCtx.Max(id) {
				return false
			}
		}
	}
	return true
}

// Sweep returns content hashes that are safe to delete from storage.
// It enumerates all blocks in the store and checks each against the
// BlockRef index. Blocks with no remaining references are candidates
// for deletion.
// The caller must verify Dominates() before calling Sweep.
func Sweep(ctx context.Context, index *BlockRef, store BlockStore) ([]string, error) {
	allBlocks, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("gc sweep: %w", err)
	}
	var unreferenced []string
	for _, hash := range allBlocks {
		if !index.IsReferenced(hash) {
			unreferenced = append(unreferenced, hash)
		}
	}
	return unreferenced, nil
}
