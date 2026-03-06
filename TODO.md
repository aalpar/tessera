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

## Next

- [ ] Delta serialization — wire-encodable deltas for real network transport
- [ ] Ring membership CRDT — track which workers are active participants
- [ ] Checkpoint + truncate — snapshot full state, discard old deltas
- [ ] S3 BlockStore backend
- [ ] Cross-geo replication layer
- [ ] Blog post / write-up — architecture diagram, reasoning
