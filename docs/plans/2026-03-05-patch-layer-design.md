# Patch Layer Design

Random-access read/write for tessera via LWW patch overlays on chunked files.

## Decisions

- **Chunk sizes in recipes:** `SnapshotRecipe.Blocks` changes from `[]string` to `[]Block{Hash, Size}` for offset computation
- **Patch semantics:** LWW per byte range, applied in timestamp order on read
- **Patch storage:** Data content-addressed in BlockStore, CRDT carries metadata only
- **CRDT composition:** `ORMap[fileID, *DotMap[patchEntry, *DotSet]]` (same nesting pattern as BlockRef)
- **GC:** Flatten re-chunks the patched file, produces a clean recipe, old patches become removable

## Data Types

```go
type Block struct {
    Hash string
    Size uint64
}

type patchEntry struct {
    FileID    string
    Offset    uint64  // byte offset in logical file
    Size      uint64  // length of patch data
    DataHash  string  // content hash in BlockStore
    Timestamp int64
    ReplicaID string
    Seq       uint64  // per-replica monotonic
}
```

PatchIndex wraps `ORMap[string, *DotMap[patchEntry, *DotSet]]`.

## Read Path

```
ReadRange(ctx, fileID, offset, length, recipe, store, patches):
  1. Walk recipe.Blocks, sum sizes → find overlapping chunks
  2. Fetch overlapping chunks from BlockStore, assemble raw bytes
  3. Query PatchIndex for fileID, filter to overlapping patches
  4. Sort patches by (Timestamp, ReplicaID, Seq)
  5. Apply in order: copy patch data over raw bytes (later overwrites earlier)
  6. Return final byte slice
```

Full file read is `ReadRange(ctx, fileID, 0, totalSize, ...)`.

## Write Path

```
WritePatch(ctx, fileID, offset, data, store, patches):
  1. Hash data, store in BlockStore
  2. Create patchEntry with offset, size, hash, timestamp, replicaID, seq
  3. Add to PatchIndex via ORMap.Apply → return delta
```

## GC Flatten

```
Flatten(ctx, fileID, recipe, store, patches, chunker):
  1. ReadRange full patched file
  2. Re-chunk through chunker → new content-defined chunks
  3. Store new chunks, register in BlockRef
  4. Return new SnapshotRecipe with []Block entries
  Caller removes flattened patches and old chunk refs.
```

Full-file re-chunk for now. Partial flatten (re-chunk only affected region) is an optimization for later.

## Testing

Unit: Block offset lookup, patch ordering, patch application (overlapping, adjacent, boundary-spanning).

Round-trip: WritePatch + ReadRange, multiple LWW patches, patch spanning chunk boundary, patch codec.

Integration: WriteSnapshot + WritePatch + ReadRange lifecycle, Flatten produces identical bytes, Flatten makes old patches unreferenced.

CRDT properties: PatchIndex merge commutativity, concurrent patches to different files, concurrent patches to same file different offsets.
