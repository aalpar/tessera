# TODO

## Done

- [x] `BlockRef` — two-level ORMap composition (`ORMap[contentHash, *DotMap[fileID, *DotSet]]`)
- [x] GC sweep with dominance check
- [x] Worker simulation (backup/delete/sync)
- [x] Add-wins safety test (concurrent add + remove)
- [x] GC dominance gate test
- [x] `BlockStore` interface + filesystem backend (content-addressed, sharded dirs)
- [x] Rabin fingerprint chunker (content-defined boundaries, `iter.Seq[Chunk]`)
- [x] `SnapshotRecipe` — immutable content-addressed file recipes
- [x] `AppendRecipe` — CRDT-based append-only stream (`AWSet[appendEntry]`)
- [x] End-to-end integration tests (backup → dedup → GC → read-back, append stream)
- [x] Delta serialization — wire-encodable deltas for real network transport
- [x] Patch layer — random-access read/write via LWW patch overlays on chunked files
  - `Block{Hash, Size}` with offset lookup helpers
  - `ReadRange` — random-access reads from chunked files
  - `PatchIndex` — CRDT (`ORMap[fileID, *DotMap[patchEntry, *DotSet]]`)
  - `WritePatch` — store patch data + record in index
  - `PatchedReadRange` — apply LWW patches over chunk data
  - `Flatten` — re-chunk patched files into clean recipes
  - PatchIndex delta serialization
  - Integration test — full lifecycle with replication
- [x] Patch GC — CompactFile: flatten + remove patches + deregister old block refs

## Next
- [ ] Ring membership CRDT — track which workers are active participants
- [ ] Checkpoint + truncate — snapshot full state, discard old deltas
- [ ] S3 BlockStore backend
- [ ] Cross-geo replication layer
- [ ] Blog post / write-up — architecture diagram, reasoning
