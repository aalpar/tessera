package tessera

import (
	"github.com/aalpar/crdt/awset"
	"github.com/aalpar/crdt/dotcontext"
)

// Ring tracks active workers using an add-wins set.
//
// Workers join explicitly; removal can be self-initiated (graceful leave)
// or peer-initiated (eviction of a stale worker). Because GC uses ring
// membership to determine which workers must be dominated before sweeping,
// removing a worker is a claim that it will not produce new BlockRef
// deltas. If a removed worker is actually alive, its next join or delta
// re-asserts presence (add-wins).
//
// Composition: AWSet[string] (worker ID)
type Ring struct {
	inner *awset.AWSet[string]
}

// NewRing creates an empty ring for the given replica.
func NewRing(replicaID string) *Ring {
	return &Ring{
		inner: awset.New[string](dotcontext.ReplicaID(replicaID)),
	}
}

// Join adds workerID to the ring and returns a delta for replication.
func (r *Ring) Join(workerID string) *Ring {
	delta := r.inner.Add(workerID)
	return &Ring{inner: delta}
}

// Remove removes workerID from the ring and returns a delta for replication.
// Used for both graceful leave (self) and eviction (peer).
func (r *Ring) Remove(workerID string) *Ring {
	delta := r.inner.Remove(workerID)
	return &Ring{inner: delta}
}

// Members returns all currently active worker IDs.
func (r *Ring) Members() []string {
	return r.inner.Elements()
}

// IsMember reports whether workerID is currently in the ring.
func (r *Ring) IsMember(workerID string) bool {
	return r.inner.Has(workerID)
}

// Len returns the number of active members.
func (r *Ring) Len() int {
	return r.inner.Len()
}

// Merge incorporates a delta from another Ring.
func (r *Ring) Merge(delta *Ring) {
	r.inner.Merge(delta.inner)
}

// Context returns the ring's causal context.
func (r *Ring) Context() *dotcontext.CausalContext {
	return r.inner.State().Context
}
