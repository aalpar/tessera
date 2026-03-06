package tessera

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// SnapshotRecipe is an immutable, content-addressed list of block hashes
// representing one version of a file.
type SnapshotRecipe struct {
	FileID  string   // logical file identity
	Version string   // content hash of this recipe (computed from Blocks)
	Blocks  []string // ordered block hashes
}

// NewSnapshotRecipe creates a recipe and computes its version hash.
func NewSnapshotRecipe(fileID string, blocks []string) *SnapshotRecipe {
	r := &SnapshotRecipe{
		FileID: fileID,
		Blocks: blocks,
	}
	r.Version = r.computeVersion()
	return r
}

func (r *SnapshotRecipe) computeVersion() string {
	h := sha256.New()
	for _, b := range r.Blocks {
		h.Write([]byte(b))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Serialize encodes the recipe as a simple text format: one block hash per line.
func (r *SnapshotRecipe) Serialize() []byte {
	var buf bytes.Buffer
	for _, b := range r.Blocks {
		buf.WriteString(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// DeserializeSnapshotRecipe parses a serialized recipe.
func DeserializeSnapshotRecipe(fileID string, data []byte) *SnapshotRecipe {
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return NewSnapshotRecipe(fileID, nil)
	}
	blocks := strings.Split(s, "\n")
	return NewSnapshotRecipe(fileID, blocks)
}

// WriteSnapshot chunks the data from r, stores blocks, registers references,
// and returns the recipe. Deltas from BlockRef.AddRef are collected for replication.
func WriteSnapshot(
	ctx context.Context,
	fileID string,
	r io.Reader,
	chunker *Chunker,
	store BlockStore,
	index *BlockRef,
) (recipe *SnapshotRecipe, deltas []*BlockRef, err error) {
	var blocks []string

	for chunk := range chunker.Chunks(r) {
		if err := store.Put(ctx, chunk.Hash, chunk.Data); err != nil {
			return nil, nil, fmt.Errorf("write snapshot %s: put block %s: %w", fileID, chunk.Hash, err)
		}
		delta := index.AddRef(chunk.Hash, fileID)
		deltas = append(deltas, delta)
		blocks = append(blocks, chunk.Hash)
	}

	recipe = NewSnapshotRecipe(fileID, blocks)

	// Store the recipe itself as a block.
	recipeData := recipe.Serialize()
	if err := store.Put(ctx, recipe.Version, recipeData); err != nil {
		return nil, nil, fmt.Errorf("write snapshot %s: put recipe: %w", fileID, err)
	}
	delta := index.AddRef(recipe.Version, fileID)
	deltas = append(deltas, delta)

	return recipe, deltas, nil
}

// ReadSnapshot reassembles a file from its recipe by reading blocks from the store.
func ReadSnapshot(ctx context.Context, recipe *SnapshotRecipe, store BlockStore) ([]byte, error) {
	var buf bytes.Buffer
	for i, hash := range recipe.Blocks {
		data, err := store.Get(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("read snapshot %s: block %d (%s): %w", recipe.FileID, i, hash, err)
		}
		buf.Write(data)
	}
	return buf.Bytes(), nil
}
