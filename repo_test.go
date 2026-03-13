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

func TestRepoGC(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if err := InitRepo(dir); err != nil {
		t.Fatal(err)
	}
	repo, err := OpenRepo(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Back up v1.
	v1 := bytes.Repeat([]byte("version-one-data "), 512)
	if err := repo.Backup(ctx, "file.txt", bytes.NewReader(v1)); err != nil {
		t.Fatal(err)
	}

	total, unref, err := repo.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("expected blocks after backup")
	}
	if unref != 0 {
		t.Fatalf("expected 0 unreferenced after backup, got %d", unref)
	}

	// Back up v2 (completely different data — v1 blocks become unreferenced).
	v2 := bytes.Repeat([]byte("version-two-data! "), 512)
	if err := repo.Backup(ctx, "file.txt", bytes.NewReader(v2)); err != nil {
		t.Fatal(err)
	}

	_, unref, err = repo.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if unref == 0 {
		t.Fatal("expected unreferenced blocks after overwrite")
	}

	// GC sweeps them.
	n, err := repo.GC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected GC to sweep blocks")
	}

	// Restore returns v2.
	got, err := repo.Restore(ctx, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, v2) {
		t.Fatal("restore after GC returned wrong data")
	}
}
