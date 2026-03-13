# GC Sweep After Merge Fix

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.
>
> **Status: ✓ Complete**

**Goal:** Fix `Sweep` so it finds unreferenced blocks regardless of whether the deletion was learned via local `Apply` or remote `Merge`.

**Architecture:** Add `List` to `BlockStore` so GC can enumerate what blocks physically exist in storage. `Sweep` compares `store.List()` against `index.IsReferenced()` — the store is the source of truth for existence, the CRDT is the source of truth for references. `UnreferencedBlocks()` is preserved for local-only use but documented as unreliable after Merge.

**Tech Stack:** Go stdlib. No new dependencies.

**Root cause:** `ORMap.Merge` (in `crdt/dotcontext/merge.go:89-91`) deletes keys where `!va.HasDots()`. After merging a remove delta, the content hash key disappears from the ORMap entirely. `UnreferencedBlocks()` iterates `Keys()` and never sees it. `ORMap.Apply` retains empty keys, so local deletes work fine.

---

### Task 1: Add `List` to `BlockStore` Interface

**Files:**
- Modify: `blockstore.go`

**Step 1: Add `List` method to the interface**

In `blockstore.go`, add `List` to the `BlockStore` interface and update the comment:

```go
// BlockStore is a content-addressable block storage interface.
// Blocks are identified by their content hash (hex-encoded SHA-256).
// Put is idempotent: storing the same hash twice is a no-op.
type BlockStore interface {
	Put(ctx context.Context, hash string, data []byte) error
	Get(ctx context.Context, hash string) ([]byte, error)
	Delete(ctx context.Context, hash string) error
	Has(ctx context.Context, hash string) (bool, error)
	List(ctx context.Context) ([]string, error)
}
```

**Step 2: Verify it fails to compile**

Run: `go build ./...`
Expected: FAIL — `FSBlockStore` does not implement `List`.

**Step 3: Commit**

```
git add blockstore.go
git commit -m "blockstore: add List method to interface

GC needs to enumerate stored blocks independently of the CRDT
metadata. UnreferencedBlocks() misses blocks whose ORMap keys
were cleaned up during Merge."
```

---

### Task 2: Implement `List` on `FSBlockStore` + Test

**Files:**
- Modify: `blockstore_fs.go`
- Modify: `blockstore_test.go`

**Step 1: Write the failing test**

In `blockstore_test.go`, add:

```go
func TestFSBlockStoreList(t *testing.T) {
	ctx := context.Background()
	store, err := NewFSBlockStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Empty store returns nothing.
	hashes, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 0 {
		t.Fatalf("expected empty list, got %v", hashes)
	}

	// Store two blocks in different shards.
	want := []string{
		"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		"ff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	for _, h := range want {
		if err := store.Put(ctx, h, []byte("data-"+h)); err != nil {
			t.Fatal(err)
		}
	}

	hashes, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(hashes)
	slices.Sort(want)
	if !slices.Equal(hashes, want) {
		t.Fatalf("List() = %v, want %v", hashes, want)
	}

	// Delete one block, verify it disappears from List.
	if err := store.Delete(ctx, want[0]); err != nil {
		t.Fatal(err)
	}
	hashes, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 1 || hashes[0] != want[1] {
		t.Fatalf("after delete: List() = %v, want [%s]", hashes, want[1])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestFSBlockStoreList -v ./...`
Expected: FAIL — `List` method not implemented.

**Step 3: Implement `List` on `FSBlockStore`**

In `blockstore_fs.go`, add import `"strings"` and the method:

```go
func (s *FSBlockStore) List(_ context.Context) ([]string, error) {
	var hashes []string
	shards, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("blockstore list: read root: %w", err)
	}
	for _, shard := range shards {
		if !shard.IsDir() || len(shard.Name()) != 2 {
			continue
		}
		prefix := shard.Name()
		files, err := os.ReadDir(filepath.Join(s.root, prefix))
		if err != nil {
			return nil, fmt.Errorf("blockstore list: read shard %s: %w", prefix, err)
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".tmp-") {
				continue
			}
			hashes = append(hashes, prefix+f.Name())
		}
	}
	return hashes, nil
}
```

Notes:
- Shards are 2-char hex prefix directories (created by `path()`).
- Skip `.tmp-*` files — leftover from interrupted `Put` operations.
- Skip non-directory entries in root and non-file entries in shards.

**Step 4: Run test to verify it passes**

Run: `go test -run TestFSBlockStoreList -v ./...`
Expected: PASS

**Step 5: Commit**

```
git add blockstore_fs.go blockstore_test.go
git commit -m "blockstore: implement List on FSBlockStore

Walks shard directories, reconstructs hashes from prefix + filename.
Skips .tmp-* files (incomplete Put operations)."
```

---

### Task 3: Change `Sweep` to Use `BlockStore.List`

**Files:**
- Modify: `gc.go`

**Step 1: Update `Sweep` signature and implementation**

Replace the existing `Sweep` function in `gc.go`:

```go
// Sweep returns content hashes that are safe to delete from storage.
// It enumerates all blocks in the store and checks each against the
// BlockRef index. Blocks with no remaining references are candidates
// for deletion.
// The caller must verify Dominates() before calling Sweep.
func Sweep(ctx context.Context, index *BlockRef, store BlockStore) ([]string, error) {
	allBlocks, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("gc sweep: %w", err)
	}
	var unreferenced []string
	for _, hash := range allBlocks {
		if !index.IsReferenced(hash) {
			unreferenced = append(unreferenced, hash)
		}
	}
	return unreferenced, nil
}
```

Add `"context"` to the imports in `gc.go`.

**Step 2: Verify it fails to compile**

Run: `go build ./...`
Expected: FAIL — existing `Sweep` callers pass wrong arguments.

**Step 3: Commit**

```
git add gc.go
git commit -m "gc: Sweep uses BlockStore.List instead of UnreferencedBlocks

The store is the source of truth for what blocks exist.
The CRDT is the source of truth for what blocks are referenced.
Sweep compares the two. This fixes GC for nodes that learn
about deletions via Merge (where ORMap cleans up empty keys)."
```

---

### Task 4: Update Existing Test Callers

Tests that use short fake hashes ("H1", "H2") without a real BlockStore should call `UnreferencedBlocks()` directly. These tests exercise BlockRef/dominance logic with local `Apply` operations, where `UnreferencedBlocks()` works correctly.

The end-to-end test (which has a real store and real hashes) switches to the new `Sweep`.

**Files:**
- Modify: `tessera_test.go`
- Modify: `ring_gc_test.go`
- Modify: `blockref.go` (doc comment)

**Step 1: Update `tessera_test.go`**

In `TestFileDeletionAndGC` (line ~85), replace:
```go
swept := Sweep(w.Index())
```
with:
```go
swept := w.Index().UnreferencedBlocks()
```

In `TestGCDominanceGate` (line ~163), replace:
```go
swept := Sweep(gcIndex)
```
with:
```go
swept := gcIndex.UnreferencedBlocks()
```

In `TestEndToEndBackupDedupGC` (line ~289), replace:
```go
swept := Sweep(gcIndex)
```
with:
```go
swept, err := Sweep(ctx, gcIndex, store)
if err != nil {
    t.Fatal(err)
}
```

Also add an assertion after the existing checks (before the `t.Logf`):
```go
if len(swept) == 0 {
    t.Fatal("expected unreferenced blocks to be swept after fileA deletion")
}
```

**Step 2: Update `ring_gc_test.go`**

In `TestGCWithRingMembership` (line ~49), replace:
```go
swept := Sweep(w1.Index())
```
with:
```go
swept := w1.Index().UnreferencedBlocks()
```

In `TestGCAfterEviction` (line ~130), replace:
```go
swept := Sweep(w1.Index())
```
with:
```go
swept := w1.Index().UnreferencedBlocks()
```

**Step 3: Document `UnreferencedBlocks` limitation**

In `blockref.go`, update the doc comment on `UnreferencedBlocks`:

```go
// UnreferencedBlocks returns content hashes with no remaining references.
//
// This only finds keys still present in the ORMap. Keys removed via
// Merge (delta replication) are cleaned up by the ORMap and will not
// appear here. For GC, use Sweep with a BlockStore instead.
func (b *BlockRef) UnreferencedBlocks() []string {
```

**Step 4: Run all tests**

Run: `go test -race ./...`
Expected: PASS — all tests compile and pass. `TestEndToEndBackupDedupGC` now sweeps > 0 blocks.

**Step 5: Commit**

```
git add tessera_test.go ring_gc_test.go blockref.go
git commit -m "gc: update callers for new Sweep signature

Unit tests with fake hashes use UnreferencedBlocks() directly
(local Apply retains empty keys, so it works for them).
End-to-end test uses Sweep(ctx, index, store) — the real fix.
Document UnreferencedBlocks limitation."
```

---

### Task 5: Add GC-After-Merge Regression Test

A focused test that exercises the exact bug scenario: a GC node receives all deltas via `Merge`, then sweeps.

**Files:**
- Modify: `tessera_test.go`

**Step 1: Write the regression test**

Add to `tessera_test.go`:

```go
// TestSweepAfterMerge verifies that GC correctly identifies unreferenced
// blocks even when the deletion was learned via Merge (not local Apply).
//
// This is a regression test: ORMap.Merge cleans up empty keys, so
// UnreferencedBlocks() would return nothing. Sweep uses BlockStore.List
// to find blocks that exist in storage but have no references.
func TestSweepAfterMerge(t *testing.T) {
	ctx := context.Background()
	store, err := NewFSBlockStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Worker backs up file-A referencing one block.
	w := NewWorker("worker-1")
	data := []byte("block-data-for-merge-test")
	hash := contentHash(data)
	if err := store.Put(ctx, hash, data); err != nil {
		t.Fatal(err)
	}
	addDeltas := w.BackupFile("file-A", []string{hash})

	// Worker deletes file-A.
	removeDeltas := w.DeleteFile("file-A", []string{hash})

	// GC node receives everything via Merge (never calls Apply).
	gcIndex := New("gc")
	for _, d := range addDeltas {
		gcIndex.Merge(d)
	}
	for _, d := range removeDeltas {
		gcIndex.Merge(d)
	}

	// Verify the ORMap bug: UnreferencedBlocks sees nothing.
	if ub := gcIndex.UnreferencedBlocks(); len(ub) != 0 {
		t.Fatalf("expected UnreferencedBlocks()=[] after merge cleanup, got %v", ub)
	}

	// Sweep using store.List finds the unreferenced block.
	swept, err := Sweep(ctx, gcIndex, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 1 || swept[0] != hash {
		t.Fatalf("expected [%s] swept, got %v", hash, swept)
	}
}
```

Note: `contentHash` is the existing unexported function in `chunk.go` that returns hex-encoded SHA-256. If it's not accessible from the test file (same package, so it should be), use `crypto/sha256` + `hex.EncodeToString` inline.

**Step 2: Run the test**

Run: `go test -run TestSweepAfterMerge -v ./...`
Expected: PASS

**Step 3: Run full suite**

Run: `go test -race ./...`
Expected: PASS

**Step 4: Commit**

```
git add tessera_test.go
git commit -m "gc: add regression test for sweep after merge

Proves that Sweep finds unreferenced blocks when deletion
was learned via Merge (ORMap cleans up empty keys).
Also asserts UnreferencedBlocks() returns nothing in this
case, documenting the limitation."
```

---

### Task 6: Update TODO

**Files:**
- Modify: `TODO.md`

**Step 1: Mark the item complete**

Change:
```
- [ ] GC sweep after merge — ...
```
to:
```
- [x] GC sweep after merge — Sweep uses BlockStore.List instead of UnreferencedBlocks
```

**Step 2: Commit**

```
git add TODO.md
git commit -m "todo: mark GC sweep after merge as done"
```

---

## Summary of Changes

| File | Change |
|------|--------|
| `blockstore.go` | Add `List` to interface, update comment |
| `blockstore_fs.go` | Implement `List` (walk shards, skip `.tmp-*`) |
| `blockstore_test.go` | `TestFSBlockStoreList` |
| `gc.go` | `Sweep(ctx, index, store)` using `List` + `IsReferenced` |
| `blockref.go` | Document `UnreferencedBlocks` limitation |
| `tessera_test.go` | Update callers, add `TestSweepAfterMerge` |
| `ring_gc_test.go` | Update callers to use `UnreferencedBlocks()` directly |
| `TODO.md` | Mark item done |

## Design Decisions

**Why `List` on BlockStore, not a tracker?** The store is the physical source of truth. A tracker would need CRDT-independent bookkeeping, bootstrap logic, and persistence — complexity without benefit at this stage. `List` is O(blocks) per GC cycle, which is acceptable for FSBlockStore. When S3BlockStore lands, GC can be optimized with a tracker if listing becomes expensive.

**Why keep `UnreferencedBlocks()`?** It works correctly for nodes that perform local deletes (Apply retains empty keys). Unit tests exercising BlockRef logic benefit from it. But it's documented as unsuitable for GC after Merge.

**Why not fix ORMap.Merge to retain empty keys?** The cleanup in Merge is intentional — it prevents unbounded key growth in the CRDT. Changing it would affect all ORMap consumers and break the algebraic guarantee that merged state only contains live data. The fix belongs in tessera (the application layer), not crdt (the algebra layer).
