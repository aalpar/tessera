package tessera

import (
	"github.com/aalpar/crdt/dotcontext"
)

// Worker simulates a backup agent that adds and removes block references.
type Worker struct {
	id    string
	index *BlockRef
}

// NewWorker creates a worker with the given replica ID.
func NewWorker(replicaID string) *Worker {
	return &Worker{
		id:    replicaID,
		index: New(replicaID),
	}
}

// BackupFile records that fileID references all given block hashes.
// Returns one delta per block for replication.
func (w *Worker) BackupFile(fileID string, blockHashes []string) []*BlockRef {
	deltas := make([]*BlockRef, len(blockHashes))
	for i, hash := range blockHashes {
		deltas[i] = w.index.AddRef(hash, fileID)
	}
	return deltas
}

// DeleteFile removes fileID's references to all given block hashes.
// Returns one delta per block for replication.
func (w *Worker) DeleteFile(fileID string, blockHashes []string) []*BlockRef {
	return w.index.RemoveFileRefs(fileID, blockHashes)
}

// Sync merges incoming deltas into the worker's local index.
func (w *Worker) Sync(deltas ...*BlockRef) {
	for _, d := range deltas {
		w.index.Merge(d)
	}
}

// Context returns the worker's causal context for GC dominance checks.
func (w *Worker) Context() *dotcontext.CausalContext {
	return w.index.Context()
}

// Index returns the worker's block reference index.
func (w *Worker) Index() *BlockRef {
	return w.index
}
