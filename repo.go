package tessera

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Repo is a local tessera repository: a block store plus metadata persisted to disk.
//
// Disk layout:
//
//	<root>/blocks/   — FSBlockStore (content-addressed, sharded)
//	<root>/blockref  — full BlockRef state, binary-encoded
//	<root>/recipes   — name→hash index, tab-separated "name\thash\n"
type Repo struct {
	store   *FSBlockStore
	index   *BlockRef
	recipes map[string]string // name → recipe hash
	root    string
}

// InitRepo creates a new empty repository at dir.
func InitRepo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	store, err := NewFSBlockStore(filepath.Join(dir, "blocks"))
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}
	r := &Repo{
		store:   store,
		index:   New("local"),
		recipes: make(map[string]string),
		root:    dir,
	}
	return r.Save()
}

// OpenRepo opens an existing repository at dir.
func OpenRepo(dir string) (*Repo, error) {
	store, err := NewFSBlockStore(filepath.Join(dir, "blocks"))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	index, err := loadIndex(filepath.Join(dir, "blockref"))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	recipes, err := loadRecipes(filepath.Join(dir, "recipes"))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return &Repo{store: store, index: index, recipes: recipes, root: dir}, nil
}

// Save persists the index and recipes to disk atomically (write+rename).
func (r *Repo) Save() error {
	if err := saveIndex(filepath.Join(r.root, "blockref"), r.index); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	if err := saveRecipes(filepath.Join(r.root, "recipes"), r.recipes); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// List returns a copy of the name→recipe-hash map.
func (r *Repo) List() map[string]string {
	out := make(map[string]string, len(r.recipes))
	for k, v := range r.recipes {
		out[k] = v
	}
	return out
}

func loadIndex(path string) (*BlockRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	delta, err := DecodeBlockRefDelta(f)
	if err != nil {
		return nil, err
	}
	// Merge into a fresh index so the live replica ID ("local") is set
	// correctly for future Apply operations. CRDT join: ∅ ⊔ S = S.
	index := New("local")
	index.Merge(delta)
	return index, nil
}

func saveIndex(path string, index *BlockRef) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := EncodeBlockRefDelta(f, index); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func loadRecipes(path string) (map[string]string, error) {
	recipes := make(map[string]string)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return recipes, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 2)
		if len(parts) == 2 {
			recipes[parts[0]] = parts[1]
		}
	}
	return recipes, sc.Err()
}

func saveRecipes(path string, recipes map[string]string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for name, hash := range recipes {
		fmt.Fprintf(w, "%s\t%s\n", name, hash)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
