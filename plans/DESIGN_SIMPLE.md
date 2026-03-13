# How Tessera Works

Imagine two laptops backing up files to the same cloud storage. Both might
chunk the same 10GB video into identical pieces — and because the chunks
are identified by their content hash, the storage only keeps one copy.
That's deduplication, and it's straightforward when there's a single
writer. But what happens when both laptops are backing up simultaneously,
deleting old versions, and a garbage collector is trying to clean up
unreferenced chunks? How do you avoid deleting a chunk that one laptop
just re-referenced a millisecond ago?

That's the problem tessera solves. And it solves it without a central
coordinator.

## The Two Layers

Tessera splits data into two layers with very different properties:

**Metadata** is small and must be everywhere. It answers: "which files
exist, and which chunks belong to them?" This is tracked by CRDTs —
data structures that any participant can modify independently, and that
converge to the same state when participants eventually sync. Metadata
replicates fully: every node eventually knows about every file.

**Blocks** are large and can live anywhere. A chunk of file data, stored
by its SHA-256 hash. Blocks don't need to be everywhere — they just need
to be *somewhere* you can fetch them from. A node that needs a block can
always get it by asking for it by hash.

This separation is the foundation. Metadata replication is the hard
distributed systems problem; block transfer is just "download this hash."

## CRDTs: Why Not Just a Database?

A quick detour, because CRDTs are the engine that makes everything else
work.

A regular database handles conflicts with locks or consensus — "only one
writer at a time" or "the majority decides." Both require coordination:
nodes must talk to each other *before* writing. If the network is down,
you wait.

CRDTs flip this. Every node writes freely, without asking permission.
When nodes sync, they merge their states using rules baked into the data
structure. The math guarantees convergence — all nodes reach the same
state regardless of what order they see updates.

Tessera uses **add-wins** semantics: if one node adds a block reference
while another concurrently removes it, the add wins. This is deliberately
conservative. It means garbage collection can never accidentally delete
a block that someone still needs. The worst case is keeping a block
slightly longer than necessary — a storage cost, not a data loss.

The concrete CRDT composition:

```
BlockRef = ORMap[contentHash → DotMap[fileID → DotSet]]
```

Read that as: "for each content hash, track which files reference it,
and witness each reference with cryptographic dots." The dots are what
let the merge function decide which adds and removes have been observed
by whom.

## Node Roles and Security Domains

Every tessera node runs in one of two roles: **peer/client** or
**peer/server**. The role determines who you can talk to and how.

### The Rules

- A **peer/client** peers with other peer/clients. Never with servers.
- A **peer/server** peers with other peer/servers. Never with clients.
- A peer/client can be a **client of** a peer/server — but this is a
  hierarchical relationship, not peering. The server serves the client;
  they don't replicate as equals.

This gives three relationship types:

```
peer/client ←→ peer/client     Client ring (equals, same domain)
peer/server ←→ peer/server     Server ring (equals, same domain)
peer/client  → peer/server     Client-of (asymmetric, authenticated)
```

### Security Domains

Who can talk to whom? That's decided by the **security domain** — the
unit of trust in tessera. A node can only peer with other nodes in the
same security domain.

What *defines* a security domain depends on the deployment:

| Deployment | Security domain is... | Enforcement |
|------------|-----------------------|-------------|
| Clients only (pure P2P) | The S3 bucket | Implicit — bucket access = trust |
| With servers | The server ring + discoverable token | Explicit — authentication required |

In the pure P2P case, there is no authentication protocol. If you can
read and write the S3 bucket, you're in. The bucket *is* the domain.

When servers are in the picture, each server ring defines a security
domain with a discoverable token. A client starting on the network can
discover available domains by their tokens, then authenticate into one.
Once authenticated, the client can peer with other clients in that
domain and use the domain's servers for block storage and metadata relay.

### Re-authentication

A peer/server can configure a timeout for how long an authenticated
client may operate without re-authenticating. The units of this timeout
are intentionally unspecified — they could be wallclock time, sync
rounds, operations, or anything else. When the timeout expires, the
client must re-authenticate before continuing to peer.

### One Client, One Domain

A single client process belongs to exactly one security domain. A user
who needs access to multiple domains runs separate client instances.
This keeps trust boundaries clean — no multiplexing domains within a
single process.

## Deployment Configurations

These roles compose into concrete deployment patterns:

### Pure P2P (Clients Only, Over S3)

```
┌──────────┐     ┌──────────┐
│ Client A │     │ Client B │
│ (cache)  │←───→│ (cache)  │   client ring
└────┬─────┘     └─────┬────┘
     │                 │
     └───────┬─────────┘
             │
     ┌───────┴───────┐
     │   Amazon S3   │
     │  (all blocks  │             security domain =
     │  + metadata)  │             the S3 bucket
     └───────────────┘
```

No servers. S3 is the durable store for both file chunks and CRDT
metadata. Each client has a local cache (bounded, LRU) and reads/writes
directly to S3. Clients discover each other through S3 — no direct
connections needed, no configuration beyond "here's the bucket."

### Client-Server

```
                    server ring
     ┌──────────────────────────────┐
     │                              │
┌────┴─────┐                  ┌────┴─────┐
│ Server 1 │←────peer────────→│ Server 2 │
│ domain-α │                  │ domain-α │
└────┬─────┘                  └────┬─────┘
     │ client-of                   │ client-of
     │                             │
┌────┴─────┐  ┌──────────┐  ┌────┴─────┐
│ Client A │←→│ Client B │←→│ Client C │   client ring
│ (cache)  │  │ (cache)  │  │ (cache)  │   (domain-α)
└──────────┘  └──────────┘  └──────────┘
```

Servers peer with each other (server ring) and manage storage. Clients
authenticate into the domain, then peer with each other (client ring).
Servers relay CRDT metadata in a hub-and-spoke pattern and can introduce
clients to each other for direct data transfer when they're on the same
network.

### Multi-Domain (Separate Security Domains)

```
Domain "alpha"                  Domain "beta"
┌─────────────────────┐        ┌──────────────────┐
│ S1 ←──peer──→ S2    │        │ S3               │
│ token: "alpha-xyz"  │        │ token: "beta-abc" │
│                     │        │                   │
│ clients: A, B       │        │ clients: C, D     │
└─────────────────────┘        └──────────────────┘
```

Data replicates within each domain's server ring. Servers in "alpha"
replicate all data among themselves, but they have no relationship with
servers in "beta." Client A cannot peer with Client C — different
domains. A user who needs both domains runs two client processes.

## What Stays the Same Across Configurations

The entire CRDT layer — BlockRef, Ring membership, PatchIndex — is
identical regardless of deployment. The chunker, recipes, read/write
pipeline, flatten, compact, garbage collection: all identical. The only
things that change are two plug-in interfaces:

**BlockStore** — where do blocks live?
- Pure P2P: `CachingStore(FSBlockStore, S3BlockStore)`
  (local disk cache in front of S3)
- Client-server: `CachingStore(FSBlockStore, ServerBlockStore)`
  (local disk cache in front of the server's storage)

**MetadataTransport** — how do CRDT deltas travel?
- Pure P2P: `S3Transport` (deltas written as S3 objects, read by peers)
- Client-server: `ServerTransport` (deltas sent to server, fanned out)

Both configurations build on the same `CachingStore` wrapper that layers
a fast local cache in front of a slower durable backend. The difference
is which backend.

## Piggybacking: How Metadata Stays Fresh Without Polling

Here's an architectural decision that deserves explanation, because the
obvious alternatives — polling on a timer, or push notifications — were
deliberately rejected.

The problem: a P2P node needs to check S3 for new CRDT deltas from
other peers. How often? Poll every 5 seconds? That's a wallclock timer,
and tessera avoids those. Wait for S3 event notifications? That adds
infrastructure (SNS/SQS) and a push dependency.

The solution: **piggyback metadata sync on block I/O.** Every time the
`CachingStore` talks to S3 — putting a block, fetching a block on
cache miss — it also checks for new CRDT deltas. No extra round trip;
you're already talking to S3.

```
You call: CachingStore.Put(block)
What happens:
  1. Write block to local cache
  2. Write block to S3        ← you're talking to S3 anyway
  3. Check for new deltas     ← "while we're here..."
  4. Return any new deltas to the caller for CRDT merge
```

The consequence is that sync frequency is proportional to activity.
A node doing a lot of backup work stays very fresh. An idle node is
stale — but an idle node doesn't *need* fresh metadata, because it's
not making decisions. When it wakes up and does its next operation,
it syncs. The system self-balances.

In client-server mode, the same pattern applies — every interaction
with the server is an opportunity to receive new deltas.

## Garbage Collection: The Safety Invariant

GC is where distributed systems get dangerous. Tessera's rule is simple
and absolute:

**A node may only delete an unreferenced block after proving it has
observed every event from every active participant in its ring.**

This is the "dominance check." If there are three nodes in the ring and
your causal context shows you've seen events up to sequence 47 from
node A, but node A is actually at sequence 50 — you can't prove that
sequences 48-50 don't re-reference the block you want to delete. You
must wait.

What makes this work across deployment configurations:

- In **pure P2P**, any client can attempt GC, but dominance is harder to
  achieve because sync is piggybacked (and thus slower than dedicated
  polling). A network partition doesn't cause data loss — it just
  stalls GC until the partition heals.

- In **client-server**, the server sees every delta (hub-and-spoke), so
  it achieves dominance faster than any client. The server is the
  natural GC executor. Clients *can* run GC but achieve dominance more
  slowly.

The Ring CRDT tracks who's active within a security domain. If a node
disappears, a staleness tracker counts sync rounds where that node's
sequence number doesn't advance. After a threshold, any peer can evict
the stale node from the ring — removing it from the dominance check. If
the eviction was a false positive (the node was just slow), it re-joins,
and add-wins semantics mean the re-join simply overrides the eviction.
No data loss either way.

## The Data Pipeline

A file's journey through tessera:

```
Raw file bytes
    │
    ▼
Chunker (Rabin fingerprint, content-defined boundaries)
    │
    ▼
Chunks: [{hash: "ab12...", data: [bytes], size: 65432}, ...]
    │
    ├──▶ BlockStore.Put(hash, data)     — store the chunk
    ├──▶ BlockRef.AddRef(hash, fileID)  — register the reference (CRDT)
    │
    ▼
SnapshotRecipe: {fileID: "report.pdf", blocks: [{ab12, 65432}, ...]}
    │
    ▶ Also stored as a block (the recipe itself is content-addressed)
```

The chunker uses a Rabin fingerprint sliding window, which means chunk
boundaries are determined by the content, not by position. If you insert
a paragraph at the beginning of a document, only the chunks near that
insertion change — the rest of the file produces identical chunks with
identical hashes. This is what makes deduplication effective even across
edits.

Patches add a layer: instead of re-chunking the whole file for a small
edit, you store just the changed bytes and their offset. On read,
patches are applied over the chunk data in timestamp order (last write
wins). When enough patches accumulate, you "flatten" — re-chunk the
patched file into a clean recipe, then remove the old patches.

## What's Not Built Yet

The CRDT layer (BlockRef, Ring, PatchIndex), the data pipeline (chunker,
recipes, patches, flatten, compact), the GC system (dominance check,
staleness tracker), and the codec (wire serialization) are all
implemented and tested.

What's planned next:

- `MetadataTransport` interface and implementations (S3Transport,
  ServerTransport)
- `S3BlockStore` and `CachingStore`
- `Node` — the runtime that wires everything together based on config
- Security domain discovery and authentication
- Integration tests for both deployment configurations

The existing code doesn't change. These are new components that compose
with what's already there.
