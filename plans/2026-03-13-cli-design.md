# CLI Design: `tessera` Dev/Demo Harness

**Date:** 2026-03-13
**Status:** Approved

## Goal

A single-node command-line tool for interactively exercising the tessera library — backup files, restore them, inspect state, and run GC. Dev/demo harness, not production backup software.

## Repo Structure on Disk

```
<repo>/
  blocks/      ← FSBlockStore (content-addressed, sharded dirs)
  blockref     ← full BlockRef state, binary-encoded via EncodeBlockRefDelta
  recipes      ← name→hash index, tab-separated "name\thash\n" per entry
```

The existing `EncodeBlockRefDelta`/`DecodeBlockRefDelta` codec encodes full accumulated state (not just deltas) — `State()` returns the complete `Causal[...]` including store and causal context. No new serialization needed.

## Architecture

A new `repo.go` in the `tessera` package wraps `FSBlockStore` + `BlockRef` + recipe map:

```go
type Repo struct {
    store   *FSBlockStore
    index   *BlockRef
    recipes map[string]string // name → recipe hash
    root    string
}
```

`Open(dir)` loads both files. `Save()` writes them back atomically (write to temp, rename). Commands follow the pattern: `Open` → do work → `Save`.

The CLI binary lives at `cmd/tessera/main.go`. It parses `-repo <dir>` globally and dispatches to subcommands. stdlib `flag` only — no third-party CLI framework.

## Commands

| Command | Behavior |
|---------|----------|
| `init` | Create repo dir, write zero-state blockref + empty recipes file |
| `backup <name> <src>` | Chunk → store → AddRef. If name exists, RemoveFileRefs for old recipe's blocks first, then add new refs. |
| `restore <name> <dst>` | Look up recipe hash → ReadSnapshot → write file to dst |
| `ls` | Print all name→recipe-hash entries |
| `gc` | `Sweep(ctx, index, store)` → delete swept blocks → print count |
| `status` | Print total block count (store.List) and unreferenced count (UnreferencedBlocks) |

### `backup` overwrite behavior

When `backup` is called for a name that already exists:
1. Load the old `SnapshotRecipe` from the store using the stored hash
2. Call `RemoveFileRefs(name, oldRecipe.Hashes())` to deregister old block references
3. Run `WriteSnapshot` for the new file
4. Update the recipes map with the new recipe hash

This ensures old unique blocks become unreferenced and are swept on the next `gc`.

## Error Handling

All errors print to stderr as `tessera <command>: <message>` and exit 1. The recipe map is checked before the block store — a missing name returns `restore: no backup named "foo"`, not a block-not-found error.

## Files Changed

| File | Lines | Purpose |
|------|-------|---------|
| `repo.go` | ~120 | Repo type, Open/Save/Init, recipe file encoding |
| `cmd/tessera/main.go` | ~130 | Flag parsing, subcommand dispatch |

No new dependencies. No new packages.

## Non-Goals (for now)

- Multi-node sync / delta exchange
- Ring membership
- Patch layer commands
- Watch mode / incremental backup
