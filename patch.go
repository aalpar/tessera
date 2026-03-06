package tessera

import (
	"slices"

	"github.com/aalpar/crdt/dotcontext"
	"github.com/aalpar/crdt/ormap"
)

// patchEntry is a byte-range patch applied over chunked file data.
// Patches are LWW: applied in (Timestamp, ReplicaID, Seq) order on read,
// later patches overwrite earlier bytes at overlapping offsets.
type patchEntry struct {
	FileID    string
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
	inner *ormap.ORMap[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]
}

// NewPatchIndex creates an empty PatchIndex for the given replica.
func NewPatchIndex(replicaID string) *PatchIndex {
	return &PatchIndex{
		inner: ormap.New[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]](
			dotcontext.ReplicaID(replicaID),
			joinPatchInner,
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

func comparePatchEntries(a, b patchEntry) int {
	if a.Timestamp != b.Timestamp {
		if a.Timestamp < b.Timestamp {
			return -1
		}
		return 1
	}
	if a.ReplicaID != b.ReplicaID {
		if a.ReplicaID < b.ReplicaID {
			return -1
		}
		return 1
	}
	if a.Seq != b.Seq {
		if a.Seq < b.Seq {
			return -1
		}
		return 1
	}
	return 0
}

func joinPatchInner(
	a, b dotcontext.Causal[*dotcontext.DotMap[patchEntry, *dotcontext.DotSet]],
) dotcontext.Causal[*dotcontext.DotMap[patchEntry, *dotcontext.DotSet]] {
	return dotcontext.JoinDotMap(a, b, joinPatchDotSet, dotcontext.NewDotSet)
}

func emptyPatchInner() *dotcontext.DotMap[patchEntry, *dotcontext.DotSet] {
	return dotcontext.NewDotMap[patchEntry, *dotcontext.DotSet]()
}

func joinPatchDotSet(
	a, b dotcontext.Causal[*dotcontext.DotSet],
) dotcontext.Causal[*dotcontext.DotSet] {
	return dotcontext.JoinDotSet(a, b)
}
