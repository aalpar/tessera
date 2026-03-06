# tessera

Distributed dedup storage prototype using delta-state CRDTs.

## Architecture

Tessera composes CRDTs from `github.com/aalpar/crdt` to coordinate block reference metadata across distributed backup workers, with a content-addressable storage layer for actual data.

```
Client API (backup, append, read)
    │
    ├── Chunker (Rabin fingerprint, content-defined)
    ├── Recipe Layer (SnapshotRecipe, AppendRecipe)
    ├── BlockRef (CRDT — tracks references for GC)
    │
    └── BlockStore (interface: Put/Get/Delete/Has)
            │
            └── FSBlockStore (filesystem backend)
```

### Core Composition

`BlockRef` wraps `ORMap[contentHash, *DotMap[fileID, *DotSet]]` — a two-level nested CRDT:
- Outer key: content hash (deduplicated block identity)
- Inner DotMap: file ID → DotSet (AWSet-like structure tracking which files reference a block)
- Add-wins semantics: concurrent add + remove of a block reference resolves in favor of add

### Components

| File | Purpose |
|------|---------|
| `blockstore.go` | BlockStore interface (content-addressable) |
| `blockstore_fs.go` | Filesystem backend (sharded dirs, atomic writes) |
| `chunk.go` | Chunk type + SHA-256 content hashing |
| `chunker.go` | Rabin fingerprint content-defined chunker |
| `recipe.go` | SnapshotRecipe (immutable versioned files) |
| `recipe_append.go` | AppendRecipe (CRDT append-only stream) |
| `blockref.go` | Block reference index (CRDT composition) |
| `gc.go` | GC sweep with dominance check |
| `worker.go` | Worker simulation for backup/delete operations |

### Storage Model

**BlockStore** — content-addressable block storage. Put is idempotent (dedup is free). No List method; GC candidates come from BlockRef.UnreferencedBlocks(). FSBlockStore shards by first 2 hex chars of the hash (like git objects).

**Chunker** — Rabin fingerprint sliding window with configurable min/max/target block sizes. Returns `iter.Seq[Chunk]` for streaming. Content-defined boundaries provide shift stability (inserting bytes only affects nearby chunk boundaries).

**SnapshotRecipe** — immutable ordered list of block hashes for one file version. Content-addressed (recipe itself stored as a block). Write pipeline: Reader → Chunker → BlockStore.Put → BlockRef.AddRef → Recipe.

**AppendRecipe** — CRDT-based append-only stream. Composition: `AWSet[appendEntry]` where appendEntry carries `{Hash, Timestamp, ReplicaID, Seq}`. Multiple writers append concurrently; read returns entries sorted by (Timestamp, ReplicaID, Seq) for deterministic total ordering.

### GC Safety

GC can only sweep unreferenced blocks after verifying dominance — the GC's causal context must have observed all events from all workers. This prevents deleting blocks that a worker has concurrently re-referenced.

## Testing

```sh
go test -v ./...
go test -race ./...
```

## Commits

- Direct push to master is fine at this stage
- No "Co-Authored-By" lines
