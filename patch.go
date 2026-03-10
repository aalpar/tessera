package tessera

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/aalpar/crdt/dotcontext"
	"github.com/aalpar/crdt/ormap"
)

// patchEntry is a byte-range patch applied over chunked file data.
// Patches are LWW: applied in (Timestamp, ReplicaID, Seq) order on read,
// later patches overwrite earlier bytes at overlapping offsets.
//
// The file identity is not stored here — it's the ORMap outer key.
type patchEntry struct {
	Offset    uint64 // byte offset in logical file
	Size      uint64 // length of patch data
	DataHash  string // content hash of patch data in BlockStore
	Timestamp int64
	ReplicaID string
	Seq       uint64 // per-replica monotonic
}

// PatchIndex tracks byte-range patches across files using CRDTs.
//
// Composition: ORMap[fileID, *DotMap[patchEntry, *DotSet]]
// Same nesting pattern as BlockRef.
type PatchIndex struct {
	seq   uint64
	inner *ormap.ORMap[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]
}

// NewPatchIndex creates an empty PatchIndex for the given replica.
func NewPatchIndex(replicaID string) *PatchIndex {
	return &PatchIndex{
		inner: ormap.New[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]](
			dotcontext.ReplicaID(replicaID),
			mergePatchInner,
			emptyPatchInner,
		),
	}
}

// AddPatch records a patch and returns a delta for replication.
func (p *PatchIndex) AddPatch(fileID string, entry patchEntry) *PatchIndex {
	delta := p.inner.Apply(fileID, func(id dotcontext.ReplicaID, ctx *dotcontext.CausalContext, v *dotcontext.DotMap[patchEntry, *dotcontext.DotSet], delta *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]) {
		d := ctx.Next(id)

		ds, ok := v.Get(entry)
		if !ok {
			ds = dotcontext.NewDotSet()
			v.Set(entry, ds)
		}
		ds.Add(d)

		deltaDS := dotcontext.NewDotSet()
		deltaDS.Add(d)
		delta.Set(entry, deltaDS)
	})
	return &PatchIndex{inner: delta}
}

// RemovePatches removes specific patch entries for a file and returns a delta.
// Concurrent adds from other replicas survive (add-wins semantics).
func (p *PatchIndex) RemovePatches(fileID string, entries []patchEntry) *PatchIndex {
	delta := p.inner.Apply(fileID, func(id dotcontext.ReplicaID, ctx *dotcontext.CausalContext, v *dotcontext.DotMap[patchEntry, *dotcontext.DotSet], delta *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]) {
		for _, entry := range entries {
			v.Delete(entry)
		}
	})
	return &PatchIndex{inner: delta}
}

// Patches returns all patches for the given file, sorted by
// (Timestamp, ReplicaID, Seq) for deterministic LWW application.
func (p *PatchIndex) Patches(fileID string) []patchEntry {
	v, ok := p.inner.Get(fileID)
	if !ok {
		return nil
	}
	entries := v.Keys()
	slices.SortFunc(entries, comparePatchEntries)
	return entries
}

// Merge incorporates a delta from another PatchIndex.
func (p *PatchIndex) Merge(delta *PatchIndex) {
	p.inner.Merge(delta.inner)
}

// WritePatch stores patch data in the BlockStore and records it in the PatchIndex.
// Returns a delta for replication.
func WritePatch(ctx context.Context, replicaID, fileID string, offset uint64, data []byte, store BlockStore, patches *PatchIndex) (*PatchIndex, error) {
	chunk := newChunk(data)
	if err := store.Put(ctx, chunk.Hash, chunk.Data); err != nil {
		return nil, fmt.Errorf("write patch %s offset %d: %w", fileID, offset, err)
	}

	patches.seq++
	entry := patchEntry{
		Offset:    offset,
		Size:      uint64(len(data)),
		DataHash:  chunk.Hash,
		Timestamp: time.Now().UnixMicro(),
		ReplicaID: replicaID,
		Seq:       patches.seq,
	}

	delta := patches.AddPatch(fileID, entry)
	return delta, nil
}

func comparePatchEntries(a, b patchEntry) int {
	return cmp.Or(
		cmp.Compare(a.Timestamp, b.Timestamp),
		cmp.Compare(a.ReplicaID, b.ReplicaID),
		cmp.Compare(a.Seq, b.Seq),
	)
}

func mergePatchInner(state, delta *dotcontext.DotMap[patchEntry, *dotcontext.DotSet], ctxState, ctxDelta *dotcontext.CausalContext) {
	dotcontext.MergeDotMapStore(state, delta, ctxState, ctxDelta, dotcontext.MergeDotSetStore, dotcontext.NewDotSet)
}

func emptyPatchInner() *dotcontext.DotMap[patchEntry, *dotcontext.DotSet] {
	return dotcontext.NewDotMap[patchEntry, *dotcontext.DotSet]()
}
