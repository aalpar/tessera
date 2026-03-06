package tessera

import (
	"testing"
)

func TestAppendRecipeSingleWriter(t *testing.T) {
	r := NewAppendRecipe("w1")

	d1 := r.Append("hash-a", 100)
	d2 := r.Append("hash-b", 200)
	d3 := r.Append("hash-c", 300)

	_ = d1
	_ = d2
	_ = d3

	hashes := r.Read()
	want := []string{"hash-a", "hash-b", "hash-c"}
	if len(hashes) != len(want) {
		t.Fatalf("got %d entries, want %d", len(hashes), len(want))
	}
	for i := range want {
		if hashes[i] != want[i] {
			t.Fatalf("entry %d: got %q, want %q", i, hashes[i], want[i])
		}
	}
}

func TestAppendRecipeMultiWriter(t *testing.T) {
	r1 := NewAppendRecipe("w1")
	r2 := NewAppendRecipe("w2")

	// w1 appends at t=100, w2 appends at t=150, w1 at t=200.
	d1 := r1.Append("hash-a", 100)
	d2 := r2.Append("hash-b", 150)
	d3 := r1.Append("hash-c", 200)

	// Sync: merge all deltas into both replicas.
	r1.Merge(d2)
	r2.Merge(d1)
	r2.Merge(d3)

	// Both should see the same order.
	h1 := r1.Read()
	h2 := r2.Read()

	want := []string{"hash-a", "hash-b", "hash-c"}
	assertSliceEqual(t, "r1", h1, want)
	assertSliceEqual(t, "r2", h2, want)
}

func TestAppendRecipeTimestampTiebreak(t *testing.T) {
	r1 := NewAppendRecipe("w1")
	r2 := NewAppendRecipe("w2")

	// Both append at the same timestamp.
	d1 := r1.Append("from-w1", 100)
	d2 := r2.Append("from-w2", 100)

	r1.Merge(d2)
	r2.Merge(d1)

	h1 := r1.Read()
	h2 := r2.Read()

	// Both replicas must agree on the order.
	assertSliceEqual(t, "tie", h1, h2)

	// w1 < w2 lexicographically, so w1's entry comes first.
	if h1[0] != "from-w1" || h1[1] != "from-w2" {
		t.Fatalf("expected w1 before w2 in tiebreak, got %v", h1)
	}
}

func TestAppendRecipeSyncBetweenReplicas(t *testing.T) {
	r1 := NewAppendRecipe("w1")
	r2 := NewAppendRecipe("w2")
	r3 := NewAppendRecipe("w3")

	d1 := r1.Append("a", 10)
	d2 := r2.Append("b", 20)
	d3 := r3.Append("c", 15)

	// r1 gets everything.
	r1.Merge(d2)
	r1.Merge(d3)

	// r2 gets via r1 (transitive).
	r2.Merge(d1)
	r2.Merge(d3)

	// r3 gets via r1.
	r3.Merge(d1)
	r3.Merge(d2)

	h1 := r1.Read()
	h2 := r2.Read()
	h3 := r3.Read()

	// All should agree: sorted by timestamp → a(10), c(15), b(20).
	want := []string{"a", "c", "b"}
	assertSliceEqual(t, "r1", h1, want)
	assertSliceEqual(t, "r2", h2, want)
	assertSliceEqual(t, "r3", h3, want)
}

func TestAppendRecipeIdempotentMerge(t *testing.T) {
	r1 := NewAppendRecipe("w1")
	r2 := NewAppendRecipe("w2")

	d := r1.Append("x", 100)

	// Merge the same delta twice.
	r2.Merge(d)
	r2.Merge(d)

	if r2.Len() != 1 {
		t.Fatalf("expected 1 entry after idempotent merge, got %d", r2.Len())
	}
}

func assertSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len %d != %d; got %v, want %v", label, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: index %d: got %q, want %q", label, i, got[i], want[i])
		}
	}
}
