# tessera

Distributed deduplicating storage engine. Multiple servers participate in
content-addressed block storage over the same repository, coordinating
metadata through delta-state CRDTs.

## What It Does

Tessera will split files into content-addressed blocks using Rabin-Karp
fingerprinting for variable-length chunking. When multiple backup workers
process files independently — possibly on different servers — blocks with
identical content are stored once. The deduplication metadata (which files
reference which blocks) is replicated across workers using CRDTs, so no
central coordinator is needed.

The interesting property: workers can back up files, delete files, and
garbage-collect unreferenced blocks **concurrently** without coordination,
and the system converges to a correct, consistent state. Add-wins semantics
ensure that a block reference is never silently lost due to a concurrent
deletion.

## Design

### Block Reference Index

The core data structure is a two-level CRDT composition:

```
ORMap[contentHash, *DotMap[fileID, *DotSet]]
```

- **Outer key** — content hash identifying a deduplicated block
- **Inner map** — file ID to dot set, tracking which files reference the block
- **Conflict resolution** — concurrent add + remove of the same reference resolves in favor of add

Workers mutate their local index and produce deltas. Deltas are shipped to
other workers, who merge them. No full-state transfer is needed.

### GC Safety

Garbage collection sweeps unreferenced blocks, but only after proving
**dominance**: the GC's causal context must have observed every event from
every worker. Without this gate, a concurrent `AddRef` on a remote worker
could be lost.

Network partitions stall GC — they never cause data loss.

### Security (planned)

Block encryption will use threshold secret sharing so that no single server
holds a complete decryption key. A quorum of participating servers is
required to reconstruct the key and read block content.

## Related Projects

Tessera composes libraries from the same workspace:

| Project | Role | Integration |
|---------|------|-------------|
| [crdt](https://github.com/aalpar/crdt) | Delta-state CRDT primitives (ORMap, AWSet, DotMap, etc.) | Used now — metadata replication |
| [shamir](https://github.com/aalpar/shamir) | Threshold cryptography for distributed key management | Planned — block encryption |

## Status

Early prototype (`v0.0.1`). The block reference index, GC, and worker
simulation are implemented. See [TODO.md](TODO.md) for what's next.

## Testing

```
go test ./...
go test -race ./...
```

## License

MIT
