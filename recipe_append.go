package tessera

import (
	"cmp"
	"slices"

	"github.com/aalpar/crdt/awset"
	"github.com/aalpar/crdt/dotcontext"
)

// appendEntry is a single entry in an append-only stream.
// It carries enough information for deterministic total ordering.
// The AWSet's internal dots handle add-wins CRDT semantics;
// these fields handle application-level ordering.
type appendEntry struct {
	Hash      string // block hash
	Timestamp int64  // wall-clock or logical timestamp
	ReplicaID string // writer identity
	Seq       uint64 // per-replica sequence number (monotonic)
}

// AppendRecipe is a CRDT-based append-only recipe for streaming/time-series data.
//
// Multiple writers can append concurrently. Entries are ordered by
// (Timestamp, ReplicaID, Seq) for a deterministic total order.
// Concurrent appends use add-wins semantics via the underlying AWSet.
//
// Composition: AWSet[appendEntry]
type AppendRecipe struct {
	id  string
	seq uint64 // local monotonic counter for unique entries
	set *awset.AWSet[appendEntry]
}

// NewAppendRecipe creates a new empty append recipe for the given replica.
func NewAppendRecipe(replicaID string) *AppendRecipe {
	return &AppendRecipe{
		id:  replicaID,
		set: awset.New[appendEntry](dotcontext.ReplicaID(replicaID)),
	}
}

// Append adds a block hash to the stream with the given timestamp.
// Returns a delta for replication.
func (a *AppendRecipe) Append(hash string, timestamp int64) *AppendRecipe {
	a.seq++
	entry := appendEntry{
		Hash:      hash,
		Timestamp: timestamp,
		ReplicaID: a.id,
		Seq:       a.seq,
	}
	delta := a.set.Add(entry)
	return &AppendRecipe{set: delta}
}

// Read returns all block hashes in timestamp order.
// Ties are broken by (ReplicaID, Seq) for deterministic total ordering.
func (a *AppendRecipe) Read() []string {
	entries := a.set.Elements()
	slices.SortFunc(entries, compareEntries)

	hashes := make([]string, len(entries))
	for i, e := range entries {
		hashes[i] = e.Hash
	}
	return hashes
}

// Entries returns all entries in timestamp order (for inspection/debugging).
func (a *AppendRecipe) Entries() []appendEntry {
	entries := a.set.Elements()
	slices.SortFunc(entries, compareEntries)
	return entries
}

// Len returns the number of entries in the stream.
func (a *AppendRecipe) Len() int {
	return a.set.Len()
}

// Merge incorporates a delta from another AppendRecipe.
func (a *AppendRecipe) Merge(delta *AppendRecipe) {
	a.set.Merge(delta.set)
}

func compareEntries(a, b appendEntry) int {
	if c := cmp.Compare(a.Timestamp, b.Timestamp); c != 0 {
		return c
	}
	if c := cmp.Compare(a.ReplicaID, b.ReplicaID); c != 0 {
		return c
	}
	return cmp.Compare(a.Seq, b.Seq)
}
