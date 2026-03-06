package tessera

import "context"

// BlockStore is a content-addressable block storage interface.
// Blocks are identified by their content hash (hex-encoded SHA-256).
// Put is idempotent: storing the same hash twice is a no-op.
// There is no List method — GC candidates come from BlockRef.UnreferencedBlocks().
type BlockStore interface {
	Put(ctx context.Context, hash string, data []byte) error
	Get(ctx context.Context, hash string) ([]byte, error)
	Delete(ctx context.Context, hash string) error
	Has(ctx context.Context, hash string) (bool, error)
}
