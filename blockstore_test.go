package tessera

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *FSBlockStore {
	t.Helper()
	s, err := NewFSBlockStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBlockStorePutGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	data := []byte("hello block")

	if err := s.Put(ctx, hash, data); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestBlockStoreIdempotentPut(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	data := []byte("hello block")

	if err := s.Put(ctx, hash, data); err != nil {
		t.Fatal(err)
	}
	// second put with same hash should be a no-op
	if err := s.Put(ctx, hash, data); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestBlockStoreHas(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"

	ok, err := s.Has(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected Has=false before Put")
	}

	if err := s.Put(ctx, hash, []byte("data")); err != nil {
		t.Fatal(err)
	}

	ok, err = s.Has(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected Has=true after Put")
	}
}

func TestBlockStoreDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"

	if err := s.Put(ctx, hash, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, hash); err != nil {
		t.Fatal(err)
	}

	ok, err := s.Has(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected Has=false after Delete")
	}

	// delete of non-existent block is idempotent
	if err := s.Delete(ctx, hash); err != nil {
		t.Fatal("delete of missing block should not error:", err)
	}
}

func TestBlockStoreGetMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if !errors.Is(err, ErrBlockNotFound) {
		t.Fatalf("expected ErrBlockNotFound, got %v", err)
	}
}

func TestBlockStoreConcurrentPut(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	data := []byte("concurrent data")

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = s.Put(ctx, hash, data)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	got, err := s.Get(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestFSBlockStoreList(t *testing.T) {
	ctx := context.Background()
	store, err := NewFSBlockStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Empty store returns nothing.
	hashes, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 0 {
		t.Fatalf("expected empty list, got %v", hashes)
	}

	// Store two blocks in different shards.
	want := []string{
		"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		"ff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	for _, h := range want {
		if err := store.Put(ctx, h, []byte("data-"+h)); err != nil {
			t.Fatal(err)
		}
	}

	hashes, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(hashes)
	slices.Sort(want)
	if !slices.Equal(hashes, want) {
		t.Fatalf("List() = %v, want %v", hashes, want)
	}

	// Delete one block, verify it disappears from List.
	if err := store.Delete(ctx, want[0]); err != nil {
		t.Fatal(err)
	}
	hashes, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 1 || hashes[0] != want[1] {
		t.Fatalf("after delete: List() = %v, want [%s]", hashes, want[1])
	}
}
