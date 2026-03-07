# Tessera Replication Design

How tessera layers file/block semantics on top of the `crdt` replication
model. For the underlying replication primitives (delta store, anti-entropy,
peer tracking, GC, transport, failure detection), see
`crdt/docs/plans/REPLICATION-MODEL-DESIGN.md`.

## Relationship to crdt

`tessera` depends on `crdt`. The `crdt` library provides:

- CRDT algebra (dotcontext, ORMap, AWSet, etc.)
- Replication primitives (DeltaStore, PeerTracker, GC)
- Generic replication model (partial replication, peer budget, store
  membership, trust mechanism)

`tessera` provides the application layer:

- Content model (files, blocks, recipes)
- Interest expression (what to replicate)
- Content routing (how to fetch blocks)
- Eviction policy (what to keep locally)

The `crdt` library knows nothing about files, blocks, or recipes. It
replicates CRDT state (deltas, contexts) generically. Tessera layers
file/block semantics on top.

## Content Model

Tessera's data separates into two layers:

| Layer | Contents | Size | Replication |
|-------|----------|------|-------------|
| Metadata | File names, root recipe hashes | Small | Full (CRDT) |
| Data | Content-addressed blocks | Large | Partial (on demand) |

**Metadata** lives in the CRDT layer (ORMap composition within a store).
Every participant receives all metadata through normal CRDT replication.
A node always knows which files exist and their root recipe hashes.

**Data** lives in the BlockStore (content-addressed). Blocks are fetched
on demand by clients that need them. Servers hold all blocks; clients
hold a subset.

### Recursive Recipe Tree

A file's recipe (ordered block list) may be too large for a single block.
A 16TB file with 64KB blocks produces ~250M recipe entries (~20GB
serialized). The writer chunks the recipe using the same content-defined
chunker used for file data, recursing until the root fits in one block.
Every level is blocks in the BlockStore — same content-addressing, same
dedup, same transfer primitive.

Tree depth is O(log(file_size / block_size)). For a 16TB file with 64KB
blocks: ~3 levels of indirection.

### Block Type Tag

Recipe entries carry `{Hash, Size, Type}` where Type is:

- `Data` — leaf node, file content
- `Recipe` — interior node, sub-recipe to recurse into

The parent declares the child's type; no format sniffing. The content hash
self-authenticates regardless of type.

### Writer Builds the Tree

The writer is the only participant that knows block ordering. It produces
the recursive recipe tree during the write pipeline:

1. Chunk file data into blocks (content-defined chunker)
2. Serialize the block list (recipe)
3. If the recipe exceeds a block: chunk it into segments (same chunker)
4. Recurse until the root fits in one block
5. Push all blocks (data + recipe segments) to peers
6. Root recipe hash enters the CRDT metadata via the "ready" gate

## Interest Expression

- **Metadata pushed, data pulled.** No subscription or interest declaration
  protocol. File metadata replicates through the CRDT layer to all
  participants. Block data is fetched on demand.

- **Interest granularity: file name for discovery, block for transfer.**
  The CRDT metadata layer carries file names and root recipe hashes —
  enough for any participant to know what files exist. The smallest
  transfer operation is fetching a single block by content hash.

- **Asymmetric flow matches data-as-gravity.** Clients push blocks and
  metadata to servers eagerly (durability — clients are sinking ships).
  Servers push metadata to clients (awareness of what exists). Clients
  pull blocks from servers on demand (selective consumption). Servers
  replicate fully to each other within the n-k subsystem.

## Content Routing

- **Servers are the default route.** All n-k servers hold all data. Any
  server can serve any block. Content routing within the n-k subsystem is
  a non-problem — it reduces to peer prioritization (which server, not
  who has it).

- **Race, don't route.** For client-to-client content transfer, no routing
  index or infrastructure is needed. When a client wants a block: (1) start
  a request to a server (TCP, always in flight), (2) simultaneously ask
  local peers "have X?" (UDP, single packet), (3) use whichever response
  arrives first. The server request is the fallback already in progress —
  no wallclock timeout needed. Silence from peers is safe (zero-ACK).

- **Zero infrastructure.** No bloom filters, no block-location index, no
  coordination protocol. Peers that have the block and are faster than the
  server win the race. Peers that don't have it or are slower are
  invisible. The optimization requires no new state — just one additional
  UDP query type alongside the existing peer communication.

- **LAN collaboration.** The primary beneficiary is same-network peers. Two
  clients on a LAN with UDP discovery will find each other. Local peer
  responses arrive before remote server responses. This captures the
  highest-value content routing scenario (sub-second shared file access)
  with zero additional infrastructure.

## Eviction Policy

- **Single LRU with touch-up propagation.** All blocks (data and recipe)
  share one LRU. When a block is accessed, touch it, then touch each
  ancestor up to the root. This maintains the invariant: every ancestor
  is always more recently touched than any of its descendants.

- **Structural ordering emerges from the invariant.** The LRU tail is
  always a leaf (data block), because root and interior recipe blocks are
  refreshed on every descendant access. Interior nodes are evicted only
  after all their descendants are gone — no special tier logic, just the
  touch-up rule.

- **Parent map for touch-up.** During top-down tree navigation (root ->
  segment -> data), record each block's parent(s) as local cache metadata.
  Bottom-up touch propagation uses this map. Entries are removed when a
  block is evicted. Blocks shared between files (content-addressed dedup)
  have multiple parents; touching the block propagates up all parent
  chains, refreshing all files that reference it.

- **Minimum cache size = tree depth.** The cache must hold at least the
  navigation path from root to leaf. For a 16TB file with 64KB blocks,
  tree depth is 3 — three blocks is the minimum to navigate to any data
  block. Below this, the client cannot even hold a path.

- **Pinning.** Application-level override: "keep this file local." Pinned
  files' blocks are exempt from eviction. Pinned blocks still participate
  in touch-up propagation (their ancestors stay fresh).

- **Eviction cost is always re-fetch latency, never data loss.** Any
  server holds every block. An evicted block can be re-fetched by content
  hash. Eviction policy is a cache optimization, not a correctness concern.
