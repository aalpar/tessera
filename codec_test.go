package tessera

import (
	"bytes"
	"context"
	"testing"
)

func TestBlockRefDeltaRoundTrip(t *testing.T) {
	// Create a BlockRef, add a reference, capture the delta
	br := New("w1")
	delta := br.AddRef("hash-abc", "file-1")

	// Encode the delta
	var buf bytes.Buffer
	if err := EncodeBlockRefDelta(&buf, delta); err != nil {
		t.Fatal(err)
	}

	// Decode
	got, err := DecodeBlockRefDelta(&buf)
	if err != nil {
		t.Fatal(err)
	}

	// Merge into fresh replica and verify
	br2 := New("w2")
	br2.Merge(got)

	refs := br2.Refs("hash-abc")
	if len(refs) != 1 || refs[0] != "file-1" {
		t.Errorf("expected [file-1], got %v", refs)
	}
}

func TestBlockRefDeltaRemoveRoundTrip(t *testing.T) {
	// Setup: w1 adds a ref, syncs to w2
	br1 := New("w1")
	addDelta := br1.AddRef("hash-abc", "file-1")

	br2 := New("w2")
	br2.Merge(addDelta)

	// Now w1 removes the ref
	removeDelta := br1.RemoveRef("hash-abc", "file-1")

	// Encode/decode the remove delta
	var buf bytes.Buffer
	if err := EncodeBlockRefDelta(&buf, removeDelta); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBlockRefDelta(&buf)
	if err != nil {
		t.Fatal(err)
	}

	// Merge into w2
	br2.Merge(decoded)

	// Verify: file-1 should no longer reference hash-abc
	if br2.IsReferenced("hash-abc") {
		t.Error("hash-abc should be unreferenced after remove")
	}
}

func TestAppendRecipeDeltaRoundTrip(t *testing.T) {
	ar := NewAppendRecipe("w1")
	delta := ar.Append("hash-xyz", 1000)

	var buf bytes.Buffer
	if err := EncodeAppendRecipeDelta(&buf, delta); err != nil {
		t.Fatal(err)
	}

	got, err := DecodeAppendRecipeDelta(&buf)
	if err != nil {
		t.Fatal(err)
	}

	// Merge into fresh replica and verify
	ar2 := NewAppendRecipe("w2")
	ar2.Merge(got)

	hashes := ar2.Read()
	if len(hashes) != 1 || hashes[0] != "hash-xyz" {
		t.Errorf("expected [hash-xyz], got %v", hashes)
	}
}

func TestAppendRecipeMultiDeltaRoundTrip(t *testing.T) {
	ar1 := NewAppendRecipe("w1")
	d1 := ar1.Append("hash-a", 100)
	d2 := ar1.Append("hash-b", 200)

	// Encode both deltas
	var buf1, buf2 bytes.Buffer
	if err := EncodeAppendRecipeDelta(&buf1, d1); err != nil {
		t.Fatal(err)
	}
	if err := EncodeAppendRecipeDelta(&buf2, d2); err != nil {
		t.Fatal(err)
	}

	// Decode and merge into fresh replica
	dec1, err := DecodeAppendRecipeDelta(&buf1)
	if err != nil {
		t.Fatal(err)
	}
	dec2, err := DecodeAppendRecipeDelta(&buf2)
	if err != nil {
		t.Fatal(err)
	}

	ar2 := NewAppendRecipe("w2")
	ar2.Merge(dec1)
	ar2.Merge(dec2)

	hashes := ar2.Read()
	if len(hashes) != 2 {
		t.Fatalf("expected 2 hashes, got %d", len(hashes))
	}
	// Read() returns sorted by timestamp, so hash-a (ts=100) first
	if hashes[0] != "hash-a" || hashes[1] != "hash-b" {
		t.Errorf("expected [hash-a, hash-b], got %v", hashes)
	}
}

func TestPatchIndexDeltaRoundTrip(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	pi := NewPatchIndex("w1")
	ctx := context.Background()

	_, err := WritePatch(ctx, "w1", "file-a", 100, []byte("hello"), store, pi)
	if err != nil {
		t.Fatal(err)
	}

	// Get the delta by adding another patch
	delta, err := WritePatch(ctx, "w1", "file-a", 200, []byte("world"), store, pi)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := EncodePatchIndexDelta(&buf, delta); err != nil {
		t.Fatal(err)
	}

	got, err := DecodePatchIndexDelta(&buf)
	if err != nil {
		t.Fatal(err)
	}

	// Merge into fresh replica
	pi2 := NewPatchIndex("w2")
	pi2.Merge(got)

	patches := pi2.Patches("file-a")
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch from delta, got %d", len(patches))
	}
	if patches[0].Offset != 200 {
		t.Errorf("expected offset 200, got %d", patches[0].Offset)
	}
}
