# tessera

Distributed dedup metadata prototype using delta-state CRDTs.

## Architecture

Tessera composes CRDTs from `github.com/aalpar/crdt` to coordinate block reference metadata across distributed backup workers.

### Core Composition

`BlockRef` wraps `ORMap[contentHash, *DotMap[fileID, *DotSet]]` — a two-level nested CRDT:
- Outer key: content hash (deduplicated block identity)
- Inner DotMap: file ID → DotSet (AWSet-like structure tracking which files reference a block)
- Add-wins semantics: concurrent add + remove of a block reference resolves in favor of add

### Components

| File | Purpose |
|------|---------|
| `blockref.go` | Block reference index (CRDT composition) |
| `gc.go` | GC sweep with dominance check |
| `worker.go` | Worker simulation for backup/delete operations |

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
