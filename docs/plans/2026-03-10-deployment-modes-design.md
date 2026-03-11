# Deployment Modes Design

Tessera supports two deployment configurations from a single binary:
**peer-to-peer** (clients in a ring over S3) and **client-server**
(clients register with servers that manage storage and topology).

## Mode Summary

| Concern             | P2P                              | Client-Server                        |
|---------------------|----------------------------------|--------------------------------------|
| Block storage       | S3 (shared, durable)             | Server-managed (server's choice)     |
| Local cache         | FSBlockStore (LRU, bounded)      | FSBlockStore (LRU, bounded)          |
| Metadata transport  | S3 (deltas as objects)           | Server relay (hub-and-spoke)         |
| Peer discovery      | S3 as rendezvous                 | Server introduces peers              |
| Ring membership     | Self-managed (any peer)          | CRDT-managed (any peer)              |
| Control plane       | None                             | Server sets replication parameters   |
| AWS dependency      | Yes (S3 SDK)                     | No                                   |

## Architecture

The shared core is identical in both modes. Mode selection determines
which `BlockStore` and `MetadataTransport` implementations are wired in
at startup.

```
┌─────────────────────────────────────────┐
│            Shared Core                  │
│  BlockRef . Ring . PatchIndex . Chunker │
│  GC . Recipes . Flatten . Compact       │
├───────────────┬─────────────────────────┤
│  BlockStore   │  MetadataTransport      │
│  (interface)  │  (interface)            │
├───────────────┼─────────────────────────┤
│ FSBlockStore  │ S3Transport   (p2p)     │
│ S3BlockStore  │ ServerTransport (c-s)   │
│ CachingStore  │                         │
│ ServerBlock*  │                         │
└───────────────┴─────────────────────────┘

P2P wiring:
  CachingStore(FSBlockStore, S3BlockStore) + S3Transport

Client-server client wiring:
  CachingStore(FSBlockStore, ServerBlockStore) + ServerTransport

Client-server server wiring:
  FSBlockStore (or operator's choice) + ServerTransport
```

## Configuration

Single binary, mode selected at startup.

**P2P mode requires:**
- S3 bucket (shared block store + metadata channel)
- Replica ID (self-assigned)
- Local cache path + size limit

**Client-server mode requires:**
- Server address(es)
- Replica ID
- Role: `client` or `server`
- Server-only: replication parameters (replica count, etc.)

## MetadataTransport Interface

The core new abstraction. Handles delta exchange between peers.

```go
type MetadataTransport interface {
    // Publish makes a delta available to other peers.
    // Called after each local mutation.
    Publish(delta []byte) error

    // Receive returns deltas from other peers that this node
    // hasn't yet consumed. Each call advances the consumer's
    // position -- deltas are not returned twice.
    Receive() ([][]byte, error)
}
```

**Design properties:**
- Deltas are opaque bytes. The transport doesn't interpret them.
  The existing codec layer handles encode/decode.
- Receive is pull-based. The caller decides when to check.
  This enables piggybacking in P2P mode.
- Bulk receive (all peers), not per-peer. Matches the
  "check everything when you check anything" piggybacking model.
- No ordering guarantee. CRDT merge is idempotent and commutative.
- No acknowledgment protocol. Re-delivery is safe.

## S3Transport (P2P Mode)

S3 is both the block store and the metadata replication channel.
No direct peer connections required. Clients don't need to be online
simultaneously.

### S3 Layout

```
s3://bucket/
├── blocks/          # content-addressed block data
│   ├── ab/cd1234... # sharded like FSBlockStore
│   └── ...
└── deltas/          # metadata replication channel
    ├── worker-A/
    │   ├── 000001   # sequential delta objects per replica
    │   ├── 000002
    │   └── ...
    └── worker-B/
        ├── 000001
        └── ...
```

### Operations

**Publish:** Write serialized delta to `deltas/{replicaID}/{seq}`.
Seq is a local monotonic counter per replica. One S3 PutObject per delta.

**Receive:** List `deltas/` prefixes to discover peers. For each peer,
list objects after the last consumed sequence number. Download new deltas.
Track consumed position locally as `map[replicaID]uint64`, persisted
alongside the local cache.

### Piggybacking

Receive is not called on a timer. The CachingStore calls it as a side
effect of block operations:

```
CachingStore.Put(block)  ->  S3.PutObject(block)  ->  transport.Receive()
CachingStore.Get(hash)   ->  cache miss -> S3.Get  ->  transport.Receive()
```

Every S3 round-trip is an opportunity to sync metadata. Active clients
stay fresh. Idle clients sync on their next operation.

### Cost Model

Each piggybacked Receive is one S3 ListObjectsV2 call per known peer
prefix. Delta objects are small. Block transfers dwarf delta traffic.
Old deltas accumulate until a future checkpoint+truncate mechanism
compacts them.

## ServerTransport (Client-Server Mode)

The server acts as a hub for delta exchange, with optional peer
introduction for direct replication.

### Hub-and-Spoke (default path)

```
Client A --publish--> Server --fan-out--> Client B
Client B --publish--> Server --fan-out--> Client A
```

**Publish:** Client sends serialized delta to the server via RPC/HTTP.
Server stores it and marks it for fan-out.

**Receive:** Client calls Receive when interacting with the server.
Server returns unconsumed deltas. The server tracks per-client
consumption position.

**Server-side storage:** Append-only delta log. Each client has a cursor
into this log. Same shape as S3Transport's layout, managed in-memory or
on local disk by the server process.

### Peer Introduction (optimization)

When the server detects two clients on the same network, it can return
peer addresses alongside deltas:

```
Receive() response:
  - deltas: [...]
  - peers: [{id: "worker-B", addr: "192.168.1.5:9090"}]
```

The client can race direct peer replication against the server relay --
same "race, don't route" pattern from REPLICATION-DESIGN.md. The
hub-and-spoke path is always the fallback.

### Server Role

The server is a ring member. It participates in CRDT merge like any
other replica. Its special authority is operational:
- Replication parameters (target replica count, cache budget hints)
- Cluster topology (registered clients, last-seen activity)

These are operational concerns, not CRDT state. Simple server-local
configuration, not replicated.

## CachingStore

Layers a local cache (FSBlockStore) in front of a remote store
(S3BlockStore in P2P, ServerBlockStore in client-server). Integrates
metadata sync via piggybacking.

```go
type CachingStore struct {
    local     BlockStore        // FSBlockStore
    remote    BlockStore        // S3BlockStore or ServerBlockStore
    transport MetadataTransport // piggybacked sync
    eviction  *LRU
}
```

### Operations

| Method   | Behavior                                                       |
|----------|----------------------------------------------------------------|
| `Put`    | Write to local + write-through to remote. Piggyback Receive(). |
| `Get`    | Local first. On miss, fetch remote, cache locally. Piggyback.  |
| `Has`    | Local first. On miss, check remote. No piggyback.              |
| `Delete` | Local only. Remote managed by GC at the remote level.          |

### Eviction

Follows the LRU with touch-up propagation design from
REPLICATION-DESIGN.md. Eviction cost is re-fetch latency, never data
loss -- the remote store has everything.

### Coupling Note

CachingStore couples block storage and metadata transport. This is
deliberate: the piggybacking requirement means block I/O and metadata
sync must be coordinated. Separating them would require the caller to
manually coordinate, defeating the piggybacking mechanism.

## GC Across Modes

GC safety (dominance check) works identically in both modes. The CRDT
algebra is transport-agnostic. The operational path to achieving
dominance differs.

**P2P mode:**
- Any client can run GC.
- Dominance achieved proportional to sync frequency (piggybacking).
- Partitioned clients stall GC -- dominance fails. Correct behavior.
- Staleness detection via StalenessTracker: a Receive() that shows
  no new seq for a peer bumps its staleness count.

**Client-server mode:**
- Server sees all deltas (hub-and-spoke), achieves dominance fastest.
- Servers are the most likely GC executors.
- Clients can run GC but achieve dominance more slowly.
- Same StalenessTracker mechanism; server's sync frequency is higher.

**Unchanged:** DominatesRing, GC sweep logic, the safety invariant
(no sweep without dominance).

## New Components

| Component          | Used in                  | Purpose                                    |
|--------------------|--------------------------|--------------------------------------------|
| `MetadataTransport`| Both (interface)         | Delta exchange abstraction                 |
| `S3BlockStore`     | P2P only                 | BlockStore backed by S3                    |
| `S3Transport`      | P2P only                 | MetadataTransport over S3 objects          |
| `ServerBlockStore` | Client-server clients    | BlockStore that fetches from server        |
| `ServerTransport`  | Client-server only       | MetadataTransport via server relay         |
| `CachingStore`     | Both                     | Local cache + remote backend + piggybacking|
| `Config`           | Both                     | Mode selection, implementation wiring      |

## Unchanged Components

| Component                          | Why unchanged                              |
|------------------------------------|--------------------------------------------|
| BlockRef, PatchIndex               | CRDT algebra is transport-agnostic         |
| Ring, StalenessTracker             | Already generic, work with any sync        |
| GC, DominatesRing                  | Dominance check doesn't know how deltas arrived |
| Chunker, Recipes, Flatten, Compact | Pure data pipeline, no networking          |
| Codec (serialization)              | Already produces []byte for the transport  |
