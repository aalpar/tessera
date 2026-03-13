package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/aalpar/tessera"
)

func main() {
	repoDir := flag.String("repo", "", "path to repository (required)")
	flag.Parse()
	args := flag.Args()

	if *repoDir == "" || len(args) == 0 {
		usage()
		os.Exit(1)
	}

	ctx := context.Background()
	if err := run(ctx, *repoDir, args[0], args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tessera %s: %v\n", args[0], err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dir, cmd string, args []string) error {
	switch cmd {
	case "init":
		return tessera.InitRepo(dir)

	case "backup":
		if len(args) != 2 {
			return fmt.Errorf("usage: backup <name> <src>")
		}
		repo, err := tessera.OpenRepo(dir)
		if err != nil {
			return err
		}
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		if err := repo.Backup(ctx, args[0], f); err != nil {
			return err
		}
		return repo.Save()

	case "restore":
		if len(args) != 2 {
			return fmt.Errorf("usage: restore <name> <dst>")
		}
		repo, err := tessera.OpenRepo(dir)
		if err != nil {
			return err
		}
		data, err := repo.Restore(ctx, args[0])
		if err != nil {
			return err
		}
		return os.WriteFile(args[1], data, 0o644)

	case "ls":
		repo, err := tessera.OpenRepo(dir)
		if err != nil {
			return err
		}
		for name, hash := range repo.List() {
			fmt.Printf("%s\t%s\n", name, hash)
		}
		return nil

	case "gc":
		repo, err := tessera.OpenRepo(dir)
		if err != nil {
			return err
		}
		n, err := repo.GC(ctx)
		if err != nil {
			return err
		}
		if err := repo.Save(); err != nil {
			return err
		}
		fmt.Printf("swept %d blocks\n", n)
		return nil

	case "status":
		repo, err := tessera.OpenRepo(dir)
		if err != nil {
			return err
		}
		total, unref, err := repo.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("blocks:       %d\n", total)
		fmt.Printf("unreferenced: %d\n", unref)
		fmt.Printf("backups:      %d\n", len(repo.List()))
		return nil

	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: tessera -repo <dir> <command> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  init                  create a new repository")
	fmt.Fprintln(os.Stderr, "  backup <name> <src>   back up a file")
	fmt.Fprintln(os.Stderr, "  restore <name> <dst>  restore a file")
	fmt.Fprintln(os.Stderr, "  ls                    list backups")
	fmt.Fprintln(os.Stderr, "  gc                    sweep unreferenced blocks")
	fmt.Fprintln(os.Stderr, "  status                show repository stats")
}
