package tessera

import (
	"bytes"
	"context"
	"testing"
)

func TestRepoBackupRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if err := InitRepo(dir); err != nil {
		t.Fatal(err)
	}

	data := []byte("the quick brown fox jumps over the lazy dog")

	// Backup.
	repo, err := OpenRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Backup(ctx, "fox.txt", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(); err != nil {
		t.Fatal(err)
	}

	// Restore in a fresh open (simulates a new process invocation).
	repo2, err := OpenRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo2.Restore(ctx, "fox.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("restore: got %q, want %q", got, data)
	}
}

func TestRepoRestoreMissing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := InitRepo(dir); err != nil {
		t.Fatal(err)
	}
	repo, err := OpenRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Restore(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing backup")
	}
}

func TestRepoInitOpen(t *testing.T) {
	dir := t.TempDir()

	if err := InitRepo(dir); err != nil {
		t.Fatalf("InitRepo: %v", err)
	}

	repo, err := OpenRepo(dir)
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	if len(repo.List()) != 0 {
		t.Fatalf("expected empty recipes, got %v", repo.List())
	}
}
