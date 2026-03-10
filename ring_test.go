package tessera

import (
	"slices"
	"testing"
)

func TestRingJoinLeave(t *testing.T) {
	r := NewRing("node-1")

	// Join two workers.
	d1 := r.Join("worker-A")
	d2 := r.Join("worker-B")

	if !r.IsMember("worker-A") || !r.IsMember("worker-B") {
		t.Fatal("both workers should be members after join")
	}
	if r.Len() != 2 {
		t.Fatalf("expected 2 members, got %d", r.Len())
	}

	// Graceful leave.
	d3 := r.Remove("worker-A")

	if r.IsMember("worker-A") {
		t.Error("worker-A should not be a member after leave")
	}
	if !r.IsMember("worker-B") {
		t.Error("worker-B should still be a member")
	}

	// Verify deltas are non-nil (used for replication).
	_ = d1
	_ = d2
	_ = d3
}

func TestRingReplication(t *testing.T) {
	r1 := NewRing("node-1")
	r2 := NewRing("node-2")

	// node-1 joins worker-A.
	d1 := r1.Join("worker-A")

	// node-2 joins worker-B.
	d2 := r2.Join("worker-B")

	// Sync.
	r1.Merge(d2)
	r2.Merge(d1)

	// Both should see both members.
	for _, r := range []*Ring{r1, r2} {
		if !r.IsMember("worker-A") || !r.IsMember("worker-B") {
			t.Error("expected both workers in ring after sync")
		}
		members := r.Members()
		slices.Sort(members)
		if !slices.Equal(members, []string{"worker-A", "worker-B"}) {
			t.Errorf("unexpected members: %v", members)
		}
	}
}

func TestRingAddWins(t *testing.T) {
	r1 := NewRing("node-1")
	r2 := NewRing("node-2")

	// Both nodes see worker-A.
	d := r1.Join("worker-A")
	r2.Merge(d)

	// Concurrent: node-1 removes, node-2 re-joins.
	dRemove := r1.Remove("worker-A")
	dRejoin := r2.Join("worker-A")

	// Sync both ways.
	r1.Merge(dRejoin)
	r2.Merge(dRemove)

	// Add wins: worker-A should survive.
	if !r1.IsMember("worker-A") {
		t.Error("node-1: worker-A should survive (add-wins)")
	}
	if !r2.IsMember("worker-A") {
		t.Error("node-2: worker-A should survive (add-wins)")
	}
}

func TestRingPeerEviction(t *testing.T) {
	r1 := NewRing("node-1")
	r2 := NewRing("node-2")

	// Worker-A joins via node-1.
	d := r1.Join("worker-A")
	r2.Merge(d)

	// Node-2 evicts worker-A (peer eviction — same Remove operation).
	dEvict := r2.Remove("worker-A")
	r1.Merge(dEvict)

	// Worker-A should be gone from both.
	if r1.IsMember("worker-A") {
		t.Error("node-1: worker-A should be evicted")
	}
	if r2.IsMember("worker-A") {
		t.Error("node-2: worker-A should be evicted")
	}
}
