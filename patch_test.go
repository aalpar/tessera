package tessera

import (
	"testing"
)

func TestPatchIndexAddAndPatches(t *testing.T) {
	pi := NewPatchIndex("w1")

	delta := pi.AddPatch("file-a", patchEntry{
		FileID:    "file-a",
		Offset:    100,
		Size:      10,
		DataHash:  "abc123",
		Timestamp: 1000,
		ReplicaID: "w1",
		Seq:       1,
	})

	// Merge delta into a second replica
	pi2 := NewPatchIndex("w2")
	pi2.Merge(delta)

	patches := pi2.Patches("file-a")
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	if patches[0].Offset != 100 || patches[0].DataHash != "abc123" {
		t.Errorf("unexpected patch: %+v", patches[0])
	}
}

func TestPatchIndexMultipleFiles(t *testing.T) {
	pi := NewPatchIndex("w1")

	pi.AddPatch("file-a", patchEntry{
		FileID: "file-a", Offset: 0, Size: 5, DataHash: "h1",
		Timestamp: 1, ReplicaID: "w1", Seq: 1,
	})
	pi.AddPatch("file-b", patchEntry{
		FileID: "file-b", Offset: 0, Size: 5, DataHash: "h2",
		Timestamp: 2, ReplicaID: "w1", Seq: 2,
	})

	if len(pi.Patches("file-a")) != 1 {
		t.Error("file-a should have 1 patch")
	}
	if len(pi.Patches("file-b")) != 1 {
		t.Error("file-b should have 1 patch")
	}
	if len(pi.Patches("file-c")) != 0 {
		t.Error("file-c should have no patches")
	}
}

func TestPatchIndexMergeCommutative(t *testing.T) {
	pi1 := NewPatchIndex("w1")
	pi2 := NewPatchIndex("w2")

	d1 := pi1.AddPatch("file-a", patchEntry{
		FileID: "file-a", Offset: 0, Size: 5, DataHash: "h1",
		Timestamp: 1, ReplicaID: "w1", Seq: 1,
	})
	d2 := pi2.AddPatch("file-a", patchEntry{
		FileID: "file-a", Offset: 100, Size: 5, DataHash: "h2",
		Timestamp: 2, ReplicaID: "w2", Seq: 1,
	})

	// Order 1: pi1 gets d2
	r1 := NewPatchIndex("r1")
	r1.Merge(d1)
	r1.Merge(d2)

	// Order 2: pi2 gets d1
	r2 := NewPatchIndex("r2")
	r2.Merge(d2)
	r2.Merge(d1)

	// Both should see 2 patches for file-a
	p1 := r1.Patches("file-a")
	p2 := r2.Patches("file-a")
	if len(p1) != 2 || len(p2) != 2 {
		t.Errorf("expected 2 patches each, got %d and %d", len(p1), len(p2))
	}
}
