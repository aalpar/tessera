package tessera

import (
	"bytes"
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
