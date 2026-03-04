package tessera

import (
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

// Sweep returns content hashes that are safe to delete from storage.
// The caller must verify Dominates() before calling Sweep.
func Sweep(index *BlockRef) []string {
	return index.UnreferencedBlocks()
}
