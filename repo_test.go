package tessera

import (
	"testing"
)

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
