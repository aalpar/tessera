# Patch GC Design

Clean up flattened patches and stale block references after compaction.

## Problem

`Flatten` produces a clean recipe with patch data baked in, but leaves behind:
1. Dead patch entries in PatchIndex
2. Old chunk block refs in BlockRef (may be unreferenced if not shared via dedup)
3. Patch data blocks in BlockStore (orphaned once patch entries are removed)

Items 2 and 3 are already handled by the existing block GC (`Sweep` + `BlockStore.Delete`). The new work is item 1: removing patch entries from PatchIndex after flatten.

## Design

### `RemovePatches` — PatchIndex method

```go
func (p *PatchIndex) RemovePatches(fileID string, entries []patchEntry) *PatchIndex
```

Removes specific patch entries from the inner DotMap for a file. Uses `ORMap.Apply` to delete each entry from the `DotMap[patchEntry, *DotSet]`. Returns a delta for replication.

Follows the `BlockRef.RemoveRef` pattern: the delta carries an empty store but a context containing the removed dots. Concurrent adds (with new, unobserved dots) survive the merge — add-wins semantics.

### `CompactFile` — orchestration function

```go
func CompactFile(
    ctx context.Context,
    fileID string,
    recipe *SnapshotRecipe,
    store BlockStore,
    index *BlockRef,
    patches *PatchIndex,
    chunker *Chunker,
) (newRecipe *SnapshotRecipe, deltas CompactDeltas, err error)
```

Steps:
1. Snapshot current patches for the file (`patches.Patches(fileID)`)
2. Call `Flatten` to produce new recipe + BlockRef add deltas
3. Remove flattened patches from PatchIndex via `RemovePatches`
4. Remove old block refs for old recipe's chunks via `BlockRef.RemoveFileRefs`
5. Return new recipe + all deltas bundled in `CompactDeltas`

`CompactDeltas` groups the deltas by type so the caller can replicate them:

```go
type CompactDeltas struct {
    BlockRefAdds    []*BlockRef   // new chunks registered
    BlockRefRemoves []*BlockRef   // old chunks deregistered
    PatchRemove     *PatchIndex   // flattened patches removed
}
```

### No dominance check needed

The CRDT algebra guarantees safety without a dominance gate:

- `RemovePatches` delta carries only the dots of the removed entries in its context
- A concurrent `AddPatch` from another replica generates new dots not in that context
- On merge, add-wins: the concurrent patch survives, the removed patches stay removed
- The surviving patch applies correctly to the new recipe because patches are byte-range overlays indexed by offset, not tied to specific chunks

### Concurrent flatten scenario

```
Worker A: Flatten(file-a) → new recipe, RemovePatches(P1, P2, P3)
Worker B: AddPatch(file-a, P4) concurrently

After sync:
  - P1, P2, P3 removed (dots observed by A's remove)
  - P4 survives (dot not in A's remove context)
  - P4 applies over new recipe at its byte offset → correct behavior
```

## Testing

**Unit: RemovePatches**
- Remove entries, verify they're gone from `Patches()`
- Concurrent add survives removal (add-wins property)
- Remove delta round-trips through codec

**Unit: CompactFile**
- File with patches → compact → `ReadRange` on new recipe matches patched read
- Old patches gone from PatchIndex after compact
- Old recipe's unique blocks become unreferenced in BlockRef (sweepable)
- Total file size preserved

**Integration: concurrent compact + patch**
- Worker A compacts file-a (has P1, P2)
- Worker B concurrently adds P3 to file-a
- After sync: P3 survives, P1/P2 removed
- `PatchedReadRange` on new recipe with P3 produces correct bytes

**Edge: compact file with no patches**
- CompactFile on unpatched file → new recipe identical to old (same content), no patch delta
