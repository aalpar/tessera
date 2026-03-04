# TODO

## Done

- [x] `BlockRef` — two-level ORMap composition (`ORMap[contentHash, *DotMap[fileID, *DotSet]]`)
- [x] GC sweep with dominance check
- [x] Worker simulation (backup/delete/sync)
- [x] Add-wins safety test (concurrent add + remove)
- [x] GC dominance gate test

## Next

- [ ] Delta serialization — wire-encodable deltas for real network transport
- [ ] File recipe tracking — `FileIndex` type mapping fileID → ordered list of content hashes (reconstruct files from blocks)
- [ ] Ring membership CRDT — track which workers are active participants
- [ ] Checkpoint + truncate — snapshot full state, discard old deltas
- [ ] Blog post / write-up — architecture diagram, reasoning, "why gossip is wrong for this"
