# Patch Layer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
>
> **Status: ✓ Complete**

**Goal:** Add random-access read/write to tessera via LWW patch overlays on chunked files.

**Architecture:** SnapshotRecipe gains chunk sizes for offset computation. A PatchIndex CRDT (`ORMap[fileID, AWSet[patchEntry]]`) tracks byte-range patches stored content-addressed in BlockStore. Reads apply patches in timestamp order over raw chunk data. GC flattens patches by re-chunking.

**Tech Stack:** Go stdlib + `github.com/aalpar/crdt` (dotcontext, ormap, awset). No new deps.

**Design doc:** `docs/plans/2026-03-05-patch-layer-design.md`

---

### Task 1: Add Size to Block, Update SnapshotRecipe

The recipe currently uses `[]string` for block hashes. Change to `[]Block{Hash, Size}` so we can compute byte offsets.

**Files:**
- Create: `block.go` (Block type + offset lookup helper)
- Modify: `recipe.go` (SnapshotRecipe.Blocks from `[]string` to `[]Block`, update Serialize/Deserialize/computeVersion)
- Modify: `recipe.go:62-79` (WriteSnapshot: construct Block with size from chunk)
- Modify: `recipe.go:95-105` (ReadSnapshot: use block.Hash)
- Modify: `recipe_test.go` (update any direct Blocks access)
- Create: `block_test.go`

**Step 1: Write failing test for Block and offset lookup**

In `block_test.go`:

```go
package tessera

import "testing"

func TestChunkForOffset(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
		{Hash: "bbb", Size: 200},
		{Hash: "ccc", Size: 150},
	}

	tests := []struct {
		offset    uint64
		wantIndex int
		wantInner uint64
	}{
		{0, 0, 0},       // start of first chunk
		{99, 0, 99},     // end of first chunk
		{100, 1, 0},     // start of second chunk
		{299, 1, 199},   // end of second chunk
		{300, 2, 0},     // start of third chunk
		{449, 2, 149},   // end of third chunk
	}

	for _, tt := range tests {
		idx, inner, err := ChunkForOffset(blocks, tt.offset)
		if err != nil {
			t.Errorf("offset %d: unexpected error: %v", tt.offset, err)
			continue
		}
		if idx != tt.wantIndex {
			t.Errorf("offset %d: chunk index = %d, want %d", tt.offset, idx, tt.wantIndex)
		}
		if inner != tt.wantInner {
			t.Errorf("offset %d: inner offset = %d, want %d", tt.offset, inner, tt.wantInner)
		}
	}
}

func TestChunkForOffsetOutOfRange(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
	}
	_, _, err := ChunkForOffset(blocks, 100)
	if err == nil {
		t.Error("expected error for offset beyond file end")
	}
}

func TestChunksForRange(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
		{Hash: "bbb", Size: 200},
		{Hash: "ccc", Size: 150},
	}

	// Range spanning chunks 0 and 1
	start, end, err := ChunksForRange(blocks, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if start != 0 || end != 2 {
		t.Errorf("got [%d,%d), want [0,2)", start, end)
	}

	// Range within single chunk
	start, end, err = ChunksForRange(blocks, 110, 50)
	if err != nil {
		t.Fatal(err)
	}
	if start != 1 || end != 2 {
		t.Errorf("got [%d,%d), want [1,2)", start, end)
	}

	// Range spanning all chunks
	start, end, err = ChunksForRange(blocks, 0, 450)
	if err != nil {
		t.Fatal(err)
	}
	if start != 0 || end != 3 {
		t.Errorf("got [%d,%d), want [0,3)", start, end)
	}
}

func TestTotalSize(t *testing.T) {
	blocks := []Block{
		{Hash: "aaa", Size: 100},
		{Hash: "bbb", Size: 200},
	}
	if got := TotalSize(blocks); got != 300 {
		t.Errorf("got %d, want 300", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run 'TestChunkFor|TestTotalSize' -v`
Expected: compilation error (types not defined)

**Step 3: Implement Block type and helpers**

In `block.go`:

```go
package tessera

import "fmt"

// Block is a content-addressed chunk with its size, used in recipes
// for random-access offset computation.
type Block struct {
	Hash string
	Size uint64
}

// TotalSize returns the sum of all block sizes.
func TotalSize(blocks []Block) uint64 {
	var total uint64
	for _, b := range blocks {
		total += b.Size
	}
	return total
}

// ChunkForOffset returns the chunk index and intra-chunk offset for
// the given byte offset within a file described by blocks.
func ChunkForOffset(blocks []Block, offset uint64) (index int, inner uint64, err error) {
	var cumulative uint64
	for i, b := range blocks {
		if offset < cumulative+b.Size {
			return i, offset - cumulative, nil
		}
		cumulative += b.Size
	}
	return 0, 0, fmt.Errorf("offset %d beyond file size %d", offset, cumulative)
}

// ChunksForRange returns the half-open range [start, end) of chunk indices
// that overlap the byte range [offset, offset+length).
func ChunksForRange(blocks []Block, offset, length uint64) (start, end int, err error) {
	if length == 0 {
		return 0, 0, nil
	}
	startIdx, _, err := ChunkForOffset(blocks, offset)
	if err != nil {
		return 0, 0, err
	}
	lastByte := offset + length - 1
	endIdx, _, err := ChunkForOffset(blocks, lastByte)
	if err != nil {
		return 0, 0, err
	}
	return startIdx, endIdx + 1, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run 'TestChunkFor|TestTotalSize' -v`
Expected: PASS

**Step 5: Update SnapshotRecipe to use Block**

Modify `recipe.go`:
- Change `Blocks []string` to `Blocks []Block`
- Update `Serialize` to write `hash size` per line (space-separated)
- Update `DeserializeSnapshotRecipe` to parse `hash size` lines
- Update `computeVersion` to hash both hash and size
- Update `WriteSnapshot` to create `Block{Hash: chunk.Hash, Size: uint64(len(chunk.Data))}`
- Update `ReadSnapshot` to use `block.Hash`

Also update all test files that reference `recipe.Blocks` directly. Check `recipe_test.go`, `tessera_test.go`, `recipe_append_test.go`.

**Step 6: Run full test suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`
Expected: all tests pass

**Step 7: Commit**

```bash
git add block.go block_test.go recipe.go recipe_test.go tessera_test.go
git commit -m "Add Block type with size, update SnapshotRecipe for random access"
```

---

### Task 2: ReadRange — Random Access Reads

Read an arbitrary byte range from a chunked file (no patches yet).

**Files:**
- Create: `readrange.go`
- Create: `readrange_test.go`

**Step 1: Write failing tests**

In `readrange_test.go`:

```go
package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestReadRangeFullFile(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 50*1024)
	rand.Read(data)

	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Read entire file via ReadRange
	got, err := ReadRange(ctx, recipe, store, 0, TotalSize(recipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("full file ReadRange mismatch")
	}
}

func TestReadRangePartial(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 50*1024)
	rand.Read(data)

	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Read bytes 1000-2000
	got, err := ReadRange(ctx, recipe, store, 1000, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[1000:2000]) {
		t.Fatal("partial ReadRange mismatch")
	}
}

func TestReadRangeSpansChunks(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	// Small chunks to force multiple chunks in a small file
	chunker := NewChunker(ChunkerConfig{MinSize: 256, MaxSize: 1024, TargetSize: 512})
	ctx := context.Background()

	data := make([]byte, 8*1024)
	rand.Read(data)

	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	if len(recipe.Blocks) < 3 {
		t.Skipf("need at least 3 chunks, got %d", len(recipe.Blocks))
	}

	// Read a range that spans the first two chunks
	chunkOneSize := recipe.Blocks[0].Size
	offset := chunkOneSize - 10 // 10 bytes before chunk boundary
	length := uint64(30)        // spans into second chunk

	got, err := ReadRange(ctx, recipe, store, offset, length)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[offset:offset+length]) {
		t.Fatal("cross-chunk ReadRange mismatch")
	}
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestReadRange -v`

**Step 3: Implement ReadRange**

In `readrange.go`:

```go
package tessera

import (
	"context"
	"fmt"
)

// ReadRange reads the byte range [offset, offset+length) from a file
// described by the given recipe, fetching chunks from the store.
func ReadRange(ctx context.Context, recipe *SnapshotRecipe, store BlockStore, offset, length uint64) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}

	startChunk, endChunk, err := ChunksForRange(recipe.Blocks, offset, length)
	if err != nil {
		return nil, fmt.Errorf("read range %s: %w", recipe.FileID, err)
	}

	result := make([]byte, length)
	var chunkStart uint64
	for i := range startChunk {
		chunkStart += recipe.Blocks[i].Size
	}

	for i := startChunk; i < endChunk; i++ {
		block := recipe.Blocks[i]
		data, err := store.Get(ctx, block.Hash)
		if err != nil {
			return nil, fmt.Errorf("read range %s: block %d (%s): %w", recipe.FileID, i, block.Hash, err)
		}

		// Compute overlap between this chunk and the requested range.
		chunkEnd := chunkStart + block.Size

		copyStart := max(offset, chunkStart)
		copyEnd := min(offset+length, chunkEnd)

		srcOffset := copyStart - chunkStart
		dstOffset := copyStart - offset
		copy(result[dstOffset:], data[srcOffset:srcOffset+(copyEnd-copyStart)])

		chunkStart = chunkEnd
	}

	return result, nil
}
```

Note: The `chunkStart` computation before the loop needs to sum sizes of blocks [0, startChunk). The implementer should verify this logic.

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestReadRange -v`

**Step 5: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 6: Commit**

```bash
git add readrange.go readrange_test.go
git commit -m "Add ReadRange for random-access reads from chunked files"
```

---

### Task 3: PatchIndex — CRDT Type

The patch metadata CRDT: `ORMap[fileID, *DotMap[patchEntry, *DotSet]]`.

**Files:**
- Create: `patch.go`
- Create: `patch_test.go`

**Step 1: Write failing tests**

In `patch_test.go`:

```go
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
```

**Step 2: Run to verify failure**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchIndex -v`

**Step 3: Implement PatchIndex**

In `patch.go`:

```go
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
```

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchIndex -v`

**Step 5: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 6: Commit**

```bash
git add patch.go patch_test.go
git commit -m "Add PatchIndex CRDT for byte-range patches"
```

---

### Task 4: WritePatch — Store Patch Data + Add to Index

**Files:**
- Modify: `patch.go` (add WritePatch function)
- Modify: `patch_test.go` (add WritePatch tests)

**Step 1: Write failing test**

Append to `patch_test.go`:

```go
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
```

Add `"bytes"` and `"context"` to the test file imports if not already present.

**Step 2: Run to verify failure**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestWritePatch -v`

**Step 3: Implement WritePatch**

Add to `patch.go`:

```go
// WritePatch stores patch data in the BlockStore and records it in the PatchIndex.
// Returns a delta for replication.
func WritePatch(ctx context.Context, replicaID, fileID string, offset uint64, data []byte, store BlockStore, patches *PatchIndex) (*PatchIndex, error) {
	chunk := newChunk(data)
	if err := store.Put(ctx, chunk.Hash, chunk.Data); err != nil {
		return nil, fmt.Errorf("write patch %s offset %d: %w", fileID, offset, err)
	}

	patches.seq++
	entry := patchEntry{
		FileID:    fileID,
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
```

This requires adding a `seq` field to PatchIndex and importing `"fmt"` and `"time"`. Update the PatchIndex struct:

```go
type PatchIndex struct {
	id    string
	seq   uint64
	inner *ormap.ORMap[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]
}
```

Update `NewPatchIndex` to set `id: replicaID`.

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestWritePatch -v`

**Step 5: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 6: Commit**

```bash
git add patch.go patch_test.go
git commit -m "Add WritePatch — store patch data and record in PatchIndex"
```

---

### Task 5: Patched ReadRange — Apply Patches Over Chunk Data

Extend ReadRange to accept a PatchIndex and apply patches.

**Files:**
- Modify: `readrange.go` (add PatchedReadRange or extend ReadRange)
- Modify: `readrange_test.go` (add patched read tests)

**Step 1: Write failing tests**

Append to `readrange_test.go`:

```go
func TestPatchedReadRange(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Write a file
	data := make([]byte, 20*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Write a patch at offset 1000: overwrite 10 bytes
	patchData := []byte("XXXXXXXXXX")
	_, err = WritePatch(ctx, "w1", "file-a", 1000, patchData, store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read patched region
	got, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 995, 20)
	if err != nil {
		t.Fatal(err)
	}

	// First 5 bytes should be original data
	if !bytes.Equal(got[:5], data[995:1000]) {
		t.Error("pre-patch bytes mismatch")
	}
	// Next 10 bytes should be patch data
	if !bytes.Equal(got[5:15], patchData) {
		t.Error("patched bytes mismatch")
	}
	// Last 5 bytes should be original data
	if !bytes.Equal(got[15:], data[1010:1015]) {
		t.Error("post-patch bytes mismatch")
	}
}

func TestPatchedReadRangeOverlapping(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 10*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Earlier patch: write "AAAA" at offset 100
	_, err = WritePatch(ctx, "w1", "file-a", 100, []byte("AAAA"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Later patch: write "BB" at offset 102 (overlaps with first patch)
	_, err = WritePatch(ctx, "w1", "file-a", 102, []byte("BB"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read the patched region
	got, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 100, 4)
	if err != nil {
		t.Fatal(err)
	}

	// Expected: "AA" from first patch, then "BB" from second (overwrites positions 102-103)
	if string(got) != "AABB" {
		t.Errorf("expected AABB, got %q", string(got))
	}
}

func TestPatchedReadRangeNoPatches(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 10*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Read without any patches — should match original
	got, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 500, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[500:600]) {
		t.Fatal("unpatched read mismatch")
	}
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchedReadRange -v`

**Step 3: Implement PatchedReadRange**

Add to `readrange.go`:

```go
// PatchedReadRange reads a byte range from a file, applying any patches
// from the PatchIndex in timestamp order (LWW semantics).
func PatchedReadRange(ctx context.Context, recipe *SnapshotRecipe, store BlockStore, patches *PatchIndex, fileID string, offset, length uint64) ([]byte, error) {
	// Read raw chunk data
	result, err := ReadRange(ctx, recipe, store, offset, length)
	if err != nil {
		return nil, err
	}

	// Apply patches in timestamp order
	for _, patch := range patches.Patches(fileID) {
		applyPatch(result, offset, length, patch, store, ctx)
	}

	return result, nil
}

func applyPatch(buf []byte, rangeOffset, rangeLength uint64, patch patchEntry, store BlockStore, ctx context.Context) {
	// Compute overlap between patch and read range
	patchEnd := patch.Offset + patch.Size
	rangeEnd := rangeOffset + rangeLength

	overlapStart := max(patch.Offset, rangeOffset)
	overlapEnd := min(patchEnd, rangeEnd)
	if overlapStart >= overlapEnd {
		return // no overlap
	}

	// Fetch patch data from store
	patchData, err := store.Get(ctx, patch.DataHash)
	if err != nil {
		return // skip patches with missing data
	}

	srcOffset := overlapStart - patch.Offset
	dstOffset := overlapStart - rangeOffset
	copyLen := overlapEnd - overlapStart
	copy(buf[dstOffset:dstOffset+copyLen], patchData[srcOffset:srcOffset+copyLen])
}
```

Note: The error handling in `applyPatch` silently skips missing patch data. The implementer should decide if this should return an error instead. For now, silent skip is acceptable for patches whose data hasn't replicated yet.

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchedReadRange -v`

**Step 5: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 6: Commit**

```bash
git add readrange.go readrange_test.go
git commit -m "Add PatchedReadRange — apply LWW patches over chunk data"
```

---

### Task 6: Flatten — GC Patches Into Clean Recipe

**Files:**
- Create: `flatten.go`
- Create: `flatten_test.go`

**Step 1: Write failing test**

In `flatten_test.go`:

```go
package tessera

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestFlattenProducesSameBytes(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	// Write original file
	data := make([]byte, 30*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	// Apply a patch
	patchData := []byte("PATCHED DATA HERE!!")
	_, err = WritePatch(ctx, "w1", "file-a", 5000, patchData, store, patches)
	if err != nil {
		t.Fatal(err)
	}

	// Read patched file
	patchedData, err := PatchedReadRange(ctx, recipe, store, patches, "file-a", 0, TotalSize(recipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}

	// Flatten
	newRecipe, _, err := Flatten(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Read from new recipe (no patches) should equal patched read
	flatData, err := ReadRange(ctx, newRecipe, store, 0, TotalSize(newRecipe.Blocks))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(flatData, patchedData) {
		t.Fatal("flattened data doesn't match patched data")
	}
}

func TestFlattenNewRecipeHasSizes(t *testing.T) {
	store, _ := NewFSBlockStore(t.TempDir())
	index := New("w1")
	patches := NewPatchIndex("w1")
	chunker := NewChunker(DefaultChunkerConfig())
	ctx := context.Background()

	data := make([]byte, 20*1024)
	rand.Read(data)
	recipe, _, err := WriteSnapshot(ctx, "file-a", bytes.NewReader(data), chunker, store, index)
	if err != nil {
		t.Fatal(err)
	}

	_, err = WritePatch(ctx, "w1", "file-a", 1000, []byte("XX"), store, patches)
	if err != nil {
		t.Fatal(err)
	}

	newRecipe, _, err := Flatten(ctx, "file-a", recipe, store, index, patches, chunker)
	if err != nil {
		t.Fatal(err)
	}

	// Every block should have a non-zero size
	for i, b := range newRecipe.Blocks {
		if b.Size == 0 {
			t.Errorf("block %d has zero size", i)
		}
	}

	// Total size should match original
	if TotalSize(newRecipe.Blocks) != TotalSize(recipe.Blocks) {
		t.Errorf("total size changed: %d vs %d", TotalSize(newRecipe.Blocks), TotalSize(recipe.Blocks))
	}
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestFlatten -v`

**Step 3: Implement Flatten**

In `flatten.go`:

```go
package tessera

import (
	"bytes"
	"context"
	"fmt"
)

// Flatten reads the full patched file, re-chunks it, and produces a clean
// recipe with no patches. Returns the new recipe and BlockRef deltas for
// the new chunks.
//
// The caller is responsible for:
// - Removing flattened patches from the PatchIndex
// - Removing old chunk references from BlockRef
func Flatten(
	ctx context.Context,
	fileID string,
	recipe *SnapshotRecipe,
	store BlockStore,
	index *BlockRef,
	patches *PatchIndex,
	chunker *Chunker,
) (*SnapshotRecipe, []*BlockRef, error) {
	totalSize := TotalSize(recipe.Blocks)
	data, err := PatchedReadRange(ctx, recipe, store, patches, fileID, 0, totalSize)
	if err != nil {
		return nil, nil, fmt.Errorf("flatten %s: read patched data: %w", fileID, err)
	}

	// Re-chunk and store
	newRecipe, deltas, err := WriteSnapshot(ctx, fileID, bytes.NewReader(data), chunker, store, index)
	if err != nil {
		return nil, nil, fmt.Errorf("flatten %s: write snapshot: %w", fileID, err)
	}

	return newRecipe, deltas, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestFlatten -v`

**Step 5: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 6: Commit**

```bash
git add flatten.go flatten_test.go
git commit -m "Add Flatten — re-chunk patched files into clean recipes"
```

---

### Task 7: PatchIndex Delta Serialization

Wire-encode PatchIndex deltas using the codec framework from earlier.

**Files:**
- Modify: `codec.go` (add patchEntryCodec, PatchIndex encode/decode)
- Modify: `codec_test.go` (add round-trip test)

**Step 1: Write failing test**

Append to `codec_test.go`:

```go
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
```

**Step 2: Run to verify failure**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchIndexDelta -v`

**Step 3: Implement patchEntryCodec and PatchIndex codec**

Add to `codec.go`:

```go
// patchEntryCodec encodes a patchEntry.
// Fields: FileID (string), Offset (uint64), Size (uint64), DataHash (string),
//         Timestamp (int64), ReplicaID (string), Seq (uint64)
type patchEntryCodec struct{}

func (patchEntryCodec) Encode(w io.Writer, e patchEntry) error {
	if err := (dotcontext.StringCodec{}).Encode(w, e.FileID); err != nil {
		return err
	}
	if err := (dotcontext.Uint64Codec{}).Encode(w, e.Offset); err != nil {
		return err
	}
	if err := (dotcontext.Uint64Codec{}).Encode(w, e.Size); err != nil {
		return err
	}
	if err := (dotcontext.StringCodec{}).Encode(w, e.DataHash); err != nil {
		return err
	}
	if err := (dotcontext.Int64Codec{}).Encode(w, e.Timestamp); err != nil {
		return err
	}
	if err := (dotcontext.StringCodec{}).Encode(w, e.ReplicaID); err != nil {
		return err
	}
	return (dotcontext.Uint64Codec{}).Encode(w, e.Seq)
}

func (patchEntryCodec) Decode(r io.Reader) (patchEntry, error) {
	var e patchEntry
	var err error
	e.FileID, err = (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Offset, err = (dotcontext.Uint64Codec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Size, err = (dotcontext.Uint64Codec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.DataHash, err = (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Timestamp, err = (dotcontext.Int64Codec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.ReplicaID, err = (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Seq, err = (dotcontext.Uint64Codec{}).Decode(r)
	return e, err
}

var patchIndexStoreCodec = &dotcontext.DotMapCodec[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]{
	KeyCodec: dotcontext.StringCodec{},
	ValueCodec: &dotcontext.DotMapCodec[patchEntry, *dotcontext.DotSet]{
		KeyCodec:   patchEntryCodec{},
		ValueCodec: dotcontext.DotSetCodec{},
	},
}

var patchIndexCausalCodec = dotcontext.CausalCodec[*dotcontext.DotMap[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]]{
	StoreCodec: patchIndexStoreCodec,
}

// EncodePatchIndexDelta encodes a PatchIndex delta for wire transport.
func EncodePatchIndexDelta(w io.Writer, delta *PatchIndex) error {
	return patchIndexCausalCodec.Encode(w, delta.inner.State())
}

// DecodePatchIndexDelta decodes a PatchIndex delta from the wire.
func DecodePatchIndexDelta(r io.Reader) (*PatchIndex, error) {
	causal, err := patchIndexCausalCodec.Decode(r)
	if err != nil {
		return nil, err
	}
	return &PatchIndex{
		inner: ormap.FromCausal(causal, joinPatchInner, emptyPatchInner),
	}, nil
}
```

Add `"github.com/aalpar/crdt/ormap"` to codec.go imports if not already present.

**Step 4: Run tests**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchIndexDelta -v`

**Step 5: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 6: Commit**

```bash
git add codec.go codec_test.go
git commit -m "Add PatchIndex delta serialization"
```

---

### Task 8: Integration Test — Full Lifecycle

End-to-end: write file, patch it, replicate patches, read from both replicas, flatten, verify.

**Files:**
- Modify: `patch_test.go` (add integration test)

**Step 1: Write the integration test**

Append to `patch_test.go`:

```go
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
```

**Step 2: Run the test**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -run TestPatchLifecycle -v`

**Step 3: Run full suite**

Run: `cd /Users/aalpar/projects/crdt-projects/tessera && go test -race ./...`

**Step 4: Commit**

```bash
git add patch_test.go
git commit -m "Add patch lifecycle integration test"
```

---

### Task 9: Final Validation + Update TODO

**Step 1: Run all tests**

Run: `cd /Users/aalpar/projects/crdt-projects && go test -race ./crdt/... ./tessera/...`

**Step 2: Run linter**

Run: `cd /Users/aalpar/projects/crdt-projects/crdt && make lint`

**Step 3: Update tessera TODO.md**

Add completed items and add new items if discovered during implementation.

**Step 4: Commit**

```bash
cd /Users/aalpar/projects/crdt-projects/tessera
git add TODO.md
git commit -m "Update TODO after patch layer implementation"
```
