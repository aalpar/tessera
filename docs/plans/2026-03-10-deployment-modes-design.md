# Deployment Modes Design

Tessera runs as a single binary with two node roles — **peer/client**
and **peer/server** — that compose into three relationship types.

## Node Roles and Relationships

Every tessera node is either a peer/client or a peer/server.

| Relationship | Description |
|-------------|-------------|
| peer/client ↔ peer/client | Client ring — equals peer with equals |
| peer/server ↔ peer/server | Server ring — equals peer with equals |
| peer/client → peer/server | Client-of — asymmetric, authenticated |

Clients peer only with clients. Servers peer only with servers.
Clients can be *clients of* servers, but this is a hierarchical
relationship, not peering.

## Security Domains

A **security domain** is the unit of trust. A node can only peer with
other nodes in the same security domain.

| Deployment | Domain defined by | Enforcement |
|------------|-------------------|-------------|
| Pure P2P (clients only) | The S3 bucket | Implicit — bucket access = trust |
| With servers | Server ring + discoverable token | Explicit — authentication required |

### Server-Managed Domains

Each server ring defines a security domain with a **discoverable
token**. A client starting on the network discovers available domains
by their tokens, then authenticates into one. The authentication
mechanism is undefined but must guarantee:

1. A rogue client cannot join a server without authorization
2. A client admitted to a domain can only peer with other clients in
   that same domain — no transitive leakage

### Re-authentication Timeout

A peer/server configures how long an authenticated client may operate
without re-authenticating. The timeout units are intentionally
unspecified (wallclock, sync rounds, operations — left open). On
expiry, the client must re-authenticate before continuing to peer.

### One Client, One Domain

A single client process belongs to exactly one security domain. A
user needing multiple domains runs separate client instances.

### Multi-Domain Topology

Servers in different domains have no relationship. Data replicates
within a domain's server ring but does not cross domain boundaries.

```
Domain "alpha"                  Domain "beta"
┌─────────────────────┐        ┌──────────────────┐
│ S1 ←──peer──→ S2    │        │ S3               │
│ token: "alpha-xyz"  │        │ token: "beta-abc" │
│                     │        │                   │
│ clients: A, B       │        │ clients: C, D     │
└─────────────────────┘        └──────────────────┘

A and B can peer. C and D can peer. A cannot peer with C.
S1 replicates with S2. S1 has no relationship with S3.
```

## Deployment Configurations

### Pure P2P (Clients Only)

```
┌──────────┐     ┌──────────┐
│ Client A │←───→│ Client B │   client ring
│ (cache)  │     │ (cache)  │
└────┬─────┘     └─────┬────┘
     │                 │
     └───────┬─────────┘
             │
     ┌───────┴───────┐
     │   Amazon S3   │             security domain =
     │  (all blocks  │             the S3 bucket
     │  + metadata)  │
     └───────────────┘
```

No servers. S3 is the durable store for blocks and CRDT metadata.
Security domain is the S3 bucket (deferred enforcement). Clients
discover each other through S3.

**Configuration:**
- S3 bucket (shared block store + metadata channel)
- Replica ID (self-assigned)
- Local cache path + size limit

**Wiring:**
`CachingStore(FSBlockStore, S3BlockStore)` + `S3Transport`

### Client-Server

```
                    server ring (domain-α)
     ┌──────────────────────────────┐
     │                              │
┌────┴─────┐                  ┌────┴─────┐
│ Server 1 │←────peer────────→│ Server 2 │
└────┬─────┘                  └────┬─────┘
     │ client-of                   │ client-of
     │                             │
┌────┴─────┐  ┌──────────┐  ┌────┴─────┐
│ Client A │←→│ Client B │←→│ Client C │   client ring
│ (cache)  │  │ (cache)  │  │ (cache)  │   (domain-α)
└──────────┘  └──────────┘  └──────────┘
```

Servers peer with each other, manage storage, relay CRDT metadata
(hub-and-spoke), and track their clients. Clients authenticate into
the domain, then peer with other clients in that domain.

**Client configuration:**
- Server address(es)
- Replica ID
- Authentication credentials for domain

**Server configuration:**
- Replica ID
- Role: `server`
- Security domain token
- Replication parameters (replica count, etc.)
- Re-authentication timeout

**Client wiring:**
`CachingStore(FSBlockStore, ServerBlockStore)` + `ServerTransport`

**Server wiring:**
`FSBlockStore (or operator's choice)` + `ServerTransport`

## Architecture

The shared core is identical in all configurations. Node role
determines which `BlockStore` and `MetadataTransport` implementations
are wired in at startup.

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
```

### Ring Scoping

Rings are scoped to security domains and node roles:

- **Server ring**: one per security domain. Servers in the domain peer
  and replicate all metadata + blocks among themselves.
- **Client ring**: one per security domain. Clients in the domain peer
  and replicate metadata. Blocks are fetched from servers or S3.
- In pure P2P mode, there is only a client ring. There is no server ring.

GC dominance is checked within the appropriate ring. A server checks
dominance against the server ring. A client checks dominance against
the client ring. The rings are independent.

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

The transport is domain-scoped: a transport instance only sees deltas
from peers in its security domain.

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

The server is a member of the server ring. It participates in CRDT
merge like any other server replica. It also observes and tracks
clients in its domain, but does not join the client ring.

Its special authority is operational:
- Security domain management (token, authentication, re-auth timeout)
- Replication parameters (target replica count, cache budget hints)
- Client tracking (registered clients, last-seen activity)

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

## GC Across Configurations

GC safety (dominance check) works identically in all configurations.
The CRDT algebra is transport-agnostic. The operational path to
achieving dominance differs.

**Pure P2P (client ring):**
- Any client can run GC.
- Dominance checked against the client ring.
- Dominance achieved proportional to sync frequency (piggybacking).
- Partitioned clients stall GC -- dominance fails. Correct behavior.
- Staleness detection via StalenessTracker: a Receive() that shows
  no new seq for a peer bumps its staleness count.

**Client-server (server ring + client ring):**
- Server checks dominance against the server ring. Sees all client
  deltas (hub-and-spoke), achieves dominance fastest.
- Servers are the most likely GC executors.
- Clients check dominance against the client ring. Achievable but
  slower — deltas are relayed through the server.
- Same StalenessTracker mechanism; server's sync frequency is higher.

**Unchanged:** DominatesRing, GC sweep logic, the safety invariant
(no sweep without dominance).

## New Components

| Component           | Used in                  | Purpose                                    |
|---------------------|--------------------------|--------------------------------------------|
| `MetadataTransport` | Both (interface)         | Delta exchange abstraction                 |
| `S3BlockStore`      | P2P only                 | BlockStore backed by S3                    |
| `S3Transport`       | P2P only                 | MetadataTransport over S3 objects          |
| `ServerBlockStore`  | Client-server clients    | BlockStore that fetches from server        |
| `ServerTransport`   | Client-server only       | MetadataTransport via server relay         |
| `CachingStore`      | Both                     | Local cache + remote backend + piggybacking|
| `SecurityDomain`    | Both                     | Domain identity + authentication           |
| `Config`            | Both                     | Role selection, implementation wiring      |

## Unchanged Components

| Component                          | Why unchanged                              |
|------------------------------------|--------------------------------------------|
| BlockRef, PatchIndex               | CRDT algebra is transport-agnostic         |
| Ring, StalenessTracker             | Already generic, work with any sync        |
| GC, DominatesRing                  | Dominance check doesn't know how deltas arrived |
| Chunker, Recipes, Flatten, Compact | Pure data pipeline, no networking          |
| Codec (serialization)              | Already produces []byte for the transport  |
