package tessera

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SnapshotRecipe is an immutable, content-addressed list of blocks
// representing one version of a file.
type SnapshotRecipe struct {
	FileID  string  // logical file identity
	Version string  // content hash of this recipe (computed from Blocks)
	Blocks  []Block // ordered blocks with hash and size
}

// NewSnapshotRecipe creates a recipe and computes its version hash.
func NewSnapshotRecipe(fileID string, blocks []Block) *SnapshotRecipe {
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
		h.Write([]byte(b.Hash))
		h.Write([]byte{' '})
		h.Write([]byte(strconv.FormatUint(b.Size, 10)))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Serialize encodes the recipe as a simple text format: "hash size" per line.
func (r *SnapshotRecipe) Serialize() []byte {
	var buf bytes.Buffer
	for _, b := range r.Blocks {
		buf.WriteString(b.Hash)
		buf.WriteByte(' ')
		buf.WriteString(strconv.FormatUint(b.Size, 10))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// DeserializeSnapshotRecipe parses a serialized recipe.
func DeserializeSnapshotRecipe(fileID string, data []byte) (*SnapshotRecipe, error) {
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return NewSnapshotRecipe(fileID, nil), nil
	}
	lines := strings.Split(s, "\n")
	blocks := make([]Block, len(lines))
	for i, line := range lines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("deserialize recipe %s: line %d: expected 'hash size', got %q", fileID, i, line)
		}
		size, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("deserialize recipe %s: line %d: bad size %q: %w", fileID, i, parts[1], err)
		}
		blocks[i] = Block{Hash: parts[0], Size: size}
	}
	return NewSnapshotRecipe(fileID, blocks), nil
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
	var blocks []Block

	for chunk := range chunker.Chunks(r) {
		if err := store.Put(ctx, chunk.Hash, chunk.Data); err != nil {
			return nil, nil, fmt.Errorf("write snapshot %s: put block %s: %w", fileID, chunk.Hash, err)
		}
		delta := index.AddRef(chunk.Hash, fileID)
		deltas = append(deltas, delta)
		blocks = append(blocks, Block{Hash: chunk.Hash, Size: uint64(len(chunk.Data))})
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
	for i, block := range recipe.Blocks {
		data, err := store.Get(ctx, block.Hash)
		if err != nil {
			return nil, fmt.Errorf("read snapshot %s: block %d (%s): %w", recipe.FileID, i, block.Hash, err)
		}
		buf.Write(data)
	}
	return buf.Bytes(), nil
}
