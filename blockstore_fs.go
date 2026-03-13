package tessera

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FSBlockStore stores content-addressed blocks as files on the local filesystem.
// Blocks are sharded into subdirectories by the first 2 hex characters of the hash
// (e.g. hash "a1b2c3..." is stored at <root>/a1/b2c3...).
type FSBlockStore struct {
	root string
}

// NewFSBlockStore creates a new filesystem-backed BlockStore rooted at the given directory.
// The directory is created if it does not exist.
func NewFSBlockStore(root string) (*FSBlockStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("blockstore: create root %q: %w", root, err)
	}
	return &FSBlockStore{root: root}, nil
}

func (s *FSBlockStore) path(hash string) (dir, file string) {
	dir = filepath.Join(s.root, hash[:2])
	file = filepath.Join(dir, hash[2:])
	return
}

func (s *FSBlockStore) Put(_ context.Context, hash string, data []byte) error {
	dir, dst := s.path(hash)

	if _, err := os.Stat(dst); err == nil {
		return nil // idempotent: already exists
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("blockstore put %s: mkdir: %w", hash, err)
	}

	// Write to temp file in the shard directory, then atomic rename.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("blockstore put %s: create temp: %w", hash, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("blockstore put %s: write: %w", hash, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("blockstore put %s: close: %w", hash, err)
	}

	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("blockstore put %s: rename: %w", hash, err)
	}
	return nil
}

func (s *FSBlockStore) Get(_ context.Context, hash string) ([]byte, error) {
	_, file := s.path(hash)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("blockstore get %s: %w", hash, ErrBlockNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("blockstore get %s: %w", hash, err)
	}
	return data, nil
}

func (s *FSBlockStore) Delete(_ context.Context, hash string) error {
	_, file := s.path(hash)
	err := os.Remove(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // idempotent delete
	}
	if err != nil {
		return fmt.Errorf("blockstore delete %s: %w", hash, err)
	}
	return nil
}

func (s *FSBlockStore) Has(_ context.Context, hash string) (bool, error) {
	_, file := s.path(hash)
	_, err := os.Stat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blockstore has %s: %w", hash, err)
	}
	return true, nil
}

func (s *FSBlockStore) List(_ context.Context) ([]string, error) {
	var hashes []string
	shards, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("blockstore list: read root: %w", err)
	}
	for _, shard := range shards {
		if !shard.IsDir() || len(shard.Name()) != 2 {
			continue
		}
		prefix := shard.Name()
		files, err := os.ReadDir(filepath.Join(s.root, prefix))
		if err != nil {
			return nil, fmt.Errorf("blockstore list: read shard %s: %w", prefix, err)
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".tmp-") {
				continue
			}
			hashes = append(hashes, prefix+f.Name())
		}
	}
	return hashes, nil
}

// ErrBlockNotFound is returned when a requested block does not exist.
var ErrBlockNotFound = errors.New("block not found")
