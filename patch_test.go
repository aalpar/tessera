package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestPatchIndexAddAndPatches(t *testing.T) {
	pi := NewPatchIndex("w1")

	delta := pi.AddPatch("file-a", patchEntry{
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
		Offset: 0, Size: 5, DataHash: "h1",
		Timestamp: 1, ReplicaID: "w1", Seq: 1,
	})
	pi.AddPatch("file-b", patchEntry{
		Offset: 0, Size: 5, DataHash: "h2",
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
		Offset: 0, Size: 5, DataHash: "h1",
		Timestamp: 1, ReplicaID: "w1", Seq: 1,
	})
	d2 := pi2.AddPatch("file-a", patchEntry{
		Offset: 100, Size: 5, DataHash: "h2",
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

func TestWritePatch(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	pi := NewPatchIndex("w1")
	ctx := context.Background()

	patchData := []byte("hello world")
	delta, err := WritePatch(ctx, "w1", "file-a", 100, patchData, store, pi)
	if err != nil {
		t.Fatal(err)
	}
	if delta == nil {
		t.Fatal("expected non-nil delta")
	}

	// Patch data should be in BlockStore
	chunk := newChunk(patchData)
	got, err := store.Get(ctx, chunk.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, patchData) {
		t.Fatal("patch data mismatch in store")
	}

	// Patch should be in index
	patches := pi.Patches("file-a")
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	if patches[0].Offset != 100 || patches[0].Size != uint64(len(patchData)) {
		t.Errorf("unexpected patch: %+v", patches[0])
	}
}

func TestPatchLifecycle(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches1 := NewPatchIndex("w1")
	patches2 := NewPatchIndex("w2")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Write a file
	original := make([]byte, 40*1024)
	rand.Read(original)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(original), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Worker 1 patches at offset 5000
	patch1Data := []byte("WORKER1_PATCH")
	d1, err := WritePatch(ctx, "w1", "file-a", 5000, patch1Data, store, patches1)
	if err != nil {
		t.Fatal(err)
	}

	// Worker 2 patches at offset 10000 (non-overlapping)
	patch2Data := []byte("WORKER2_PATCH")
	d2, err := WritePatch(ctx, "w2", "file-a", 10000, patch2Data, store, patches2)
	if err != nil {
		t.Fatal(err)
	}

	// Replicate: each gets the other's delta
	patches1.Merge(d2)
	patches2.Merge(d1)

	// Both replicas should read the same patched file
	totalSize := TotalSize(recipe.Blocks)
	read1, err := PatchedReadRange(ctx, recipe, store, patches1, "file-a", 0, totalSize)
	if err != nil {
		t.Fatal(err)
	}
	read2, err := PatchedReadRange(ctx, recipe, store, patches2, "file-a", 0, totalSize)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(read1, read2) {
		t.Fatal("replicas diverge after patch sync")
	}

	// Verify patches are applied correctly
	if !bytes.Equal(read1[5000:5000+len(patch1Data)], patch1Data) {
		t.Error("patch1 not applied")
	}
	if !bytes.Equal(read1[10000:10000+len(patch2Data)], patch2Data) {
		t.Error("patch2 not applied")
	}

	// Flatten
	newRecipe, _, err := Flatten(ctx, "file-a", recipe, store, index, patches1, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Read from new recipe (no patches) should match patched read
	flatData, err := ReadRange(ctx, newRecipe, store, 0, TotalSize(newRecipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(flatData, read1) {
		t.Fatal("flattened data doesn't match patched read")
	}
}

func TestRemovePatches(t *testing.T) {
	pi := NewPatchIndex("w1")

	e1 := patchEntry{Offset: 0, Size: 5, DataHash: "h1", Timestamp: 1, ReplicaID: "w1", Seq: 1}
	e2 := patchEntry{Offset: 100, Size: 5, DataHash: "h2", Timestamp: 2, ReplicaID: "w1", Seq: 2}

	pi.AddPatch("file-a", e1)
	pi.AddPatch("file-a", e2)

	if len(pi.Patches("file-a")) != 2 {
		t.Fatal("expected 2 patches before remove")
	}

	// Remove only e1
	delta := pi.RemovePatches("file-a", []patchEntry{e1})
	if delta == nil {
		t.Fatal("expected non-nil delta")
	}

	// e1 gone, e2 remains
	patches := pi.Patches("file-a")
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch after remove, got %d", len(patches))
	}
	if patches[0].DataHash != "h2" {
		t.Errorf("expected h2 to survive, got %s", patches[0].DataHash)
	}
}

func TestRemovePatchesConcurrentAddSurvives(t *testing.T) {
	pi1 := NewPatchIndex("w1")
	pi2 := NewPatchIndex("w2")

	// w1 adds P1
	e1 := patchEntry{Offset: 0, Size: 5, DataHash: "h1", Timestamp: 1, ReplicaID: "w1", Seq: 1}
	d1 := pi1.AddPatch("file-a", e1)
	pi2.Merge(d1)

	// w1 removes P1 (simulating post-flatten cleanup)
	dRemove := pi1.RemovePatches("file-a", []patchEntry{e1})

	// w2 concurrently adds P2 (hasn't seen the remove yet)
	e2 := patchEntry{Offset: 200, Size: 10, DataHash: "h2", Timestamp: 2, ReplicaID: "w2", Seq: 1}
	dAdd := pi2.AddPatch("file-a", e2)

	// Sync: each gets the other's delta
	pi1.Merge(dAdd)
	pi2.Merge(dRemove)

	// Both should converge: P1 removed, P2 survives
	for _, pi := range []*PatchIndex{pi1, pi2} {
		patches := pi.Patches("file-a")
		if len(patches) != 1 {
			t.Fatalf("expected 1 patch, got %d", len(patches))
		}
		if patches[0].DataHash != "h2" {
			t.Errorf("expected h2 to survive, got %s", patches[0].DataHash)
		}
	}
}

func TestRemovePatchesAllForFile(t *testing.T) {
	pi := NewPatchIndex("w1")

	e1 := patchEntry{Offset: 0, Size: 5, DataHash: "h1", Timestamp: 1, ReplicaID: "w1", Seq: 1}
	e2 := patchEntry{Offset: 100, Size: 5, DataHash: "h2", Timestamp: 2, ReplicaID: "w1", Seq: 2}

	pi.AddPatch("file-a", e1)
	pi.AddPatch("file-a", e2)

	// Remove both
	pi.RemovePatches("file-a", []patchEntry{e1, e2})

	patches := pi.Patches("file-a")
	if len(patches) != 0 {
		t.Fatalf("expected 0 patches after removing all, got %d", len(patches))
	}
}
