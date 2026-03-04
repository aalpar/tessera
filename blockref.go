package tessera

import (
	"github.com/aalpar/crdt/dotcontext"
	"github.com/aalpar/crdt/ormap"
)

// BlockRef is a block reference index for deduplicated backup storage.
//
// It tracks which files reference which content-addressed blocks using
// a two-level CRDT composition:
//
//	ORMap[contentHash, *DotMap[fileID, *DotSet]]
//
// The outer ORMap provides add-wins semantics for block entries.
// The inner DotMap is an AWSet-like structure: each file's reference
// to a block is witnessed by dots in a DotSet.
type BlockRef struct {
	inner *ormap.ORMap[string, *dotcontext.DotMap[string, *dotcontext.DotSet]]
}

// New creates an empty BlockRef index for the given replica.
func New(replicaID string) *BlockRef {
	return &BlockRef{
		inner: ormap.New[string, *dotcontext.DotMap[string, *dotcontext.DotSet]](
			replicaID,
			joinInner,
			emptyInner,
		),
	}
}

// AddRef records that fileID references contentHash and returns a delta.
func (b *BlockRef) AddRef(contentHash, fileID string) *BlockRef {
	delta := b.inner.Apply(contentHash, func(id string, ctx *dotcontext.CausalContext, v *dotcontext.DotMap[string, *dotcontext.DotSet], delta *dotcontext.DotMap[string, *dotcontext.DotSet]) {
		d := ctx.Next(id)

		// Update local state: add dot to this file's dot set.
		ds, ok := v.Get(fileID)
		if !ok {
			ds = dotcontext.NewDotSet()
			v.Set(fileID, ds)
		}
		ds.Add(d)

		// Mirror into delta.
		deltaDS := dotcontext.NewDotSet()
		deltaDS.Add(d)
		delta.Set(fileID, deltaDS)
	})
	return &BlockRef{inner: delta}
}

// RemoveRef removes fileID's reference to contentHash and returns a delta.
//
// The delta carries no store entries but its context records the removed
// dots — so concurrent adds (with new, unobserved dots) survive the merge.
func (b *BlockRef) RemoveRef(contentHash, fileID string) *BlockRef {
	delta := b.inner.Apply(contentHash, func(id string, ctx *dotcontext.CausalContext, v *dotcontext.DotMap[string, *dotcontext.DotSet], delta *dotcontext.DotMap[string, *dotcontext.DotSet]) {
		v.Delete(fileID)
	})
	return &BlockRef{inner: delta}
}

// RemoveFileRefs removes fileID's references to all given content hashes.
// Returns one delta per hash.
func (b *BlockRef) RemoveFileRefs(fileID string, contentHashes []string) []*BlockRef {
	deltas := make([]*BlockRef, len(contentHashes))
	for i, hash := range contentHashes {
		deltas[i] = b.RemoveRef(hash, fileID)
	}
	return deltas
}

// IsReferenced reports whether any file references contentHash.
func (b *BlockRef) IsReferenced(contentHash string) bool {
	v, ok := b.inner.Get(contentHash)
	if !ok {
		return false
	}
	return v.Dots().Len() > 0
}

// Refs returns the file IDs that reference contentHash.
func (b *BlockRef) Refs(contentHash string) []string {
	v, ok := b.inner.Get(contentHash)
	if !ok {
		return nil
	}
	return v.Keys()
}

// UnreferencedBlocks returns content hashes with no remaining references.
func (b *BlockRef) UnreferencedBlocks() []string {
	var unreferenced []string
	for _, key := range b.inner.Keys() {
		if !b.IsReferenced(key) {
			unreferenced = append(unreferenced, key)
		}
	}
	return unreferenced
}

// Merge incorporates a delta from another BlockRef.
func (b *BlockRef) Merge(delta *BlockRef) {
	b.inner.Merge(delta.inner)
}

// Context returns the causal context of the underlying ORMap.
// Used by GC to check dominance before sweeping.
func (b *BlockRef) Context() *dotcontext.CausalContext {
	// The ORMap's state is Causal[*DotMap[...]], and its context
	// tracks all dots this replica has observed.
	return b.inner.Context()
}

// joinInner joins two Causal[*DotMap[fileID, *DotSet]] values.
// This is the recursive join: for each fileID, join the DotSets.
func joinInner(
	a, b dotcontext.Causal[*dotcontext.DotMap[string, *dotcontext.DotSet]],
) dotcontext.Causal[*dotcontext.DotMap[string, *dotcontext.DotSet]] {
	return dotcontext.JoinDotMap(a, b, joinDotSet, dotcontext.NewDotSet)
}

// emptyInner returns a new empty DotMap[fileID, *DotSet].
func emptyInner() *dotcontext.DotMap[string, *dotcontext.DotSet] {
	return dotcontext.NewDotMap[string, *dotcontext.DotSet]()
}

// joinDotSet adapts JoinDotSet to the signature required by JoinDotMap.
func joinDotSet(
	a, b dotcontext.Causal[*dotcontext.DotSet],
) dotcontext.Causal[*dotcontext.DotSet] {
	return dotcontext.JoinDotSet(a, b)
}
