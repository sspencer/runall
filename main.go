// Command runall runs the given command in every immediate subdirectory of the
// current directory, in parallel, and prints each directory's buffered output
// in alphabetical order once all commands have finished.
//
// Usage:
//
//	runall git pull
//	runall ls -la
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
)

type result struct {
	dir    string
	output bytes.Buffer
	err    error
}

const usage = `Usage: runall <command> [args...]

Runs the specified command in each immediate subdirectory, in parallel, and
prints each directory's output in alphabetical order once all have finished.

Examples:
  runall git pull
  runall make test
  runall ls -la
  runall sh -c 'git log -1 --oneline | cat'
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Print(usage)
		return
	}

	dirs, err := subdirectories(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "runall: %v\n", err)
		os.Exit(1)
	}
	if len(dirs) == 0 {
		fmt.Fprintln(os.Stderr, "runall: no subdirectories found")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	results := run(ctx, dirs, os.Args[1], os.Args[2:])

	failed := 0
	for i, r := range results {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("==> %s <==\n", r.dir)
		os.Stdout.Write(r.output.Bytes())
		if r.output.Len() > 0 && !bytes.HasSuffix(r.output.Bytes(), []byte("\n")) {
			fmt.Println()
		}
		if r.err != nil {
			failed++
			fmt.Printf("runall: %s: %v\n", r.dir, r.err)
		}
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\nrunall: %d of %d commands failed\n", failed, len(results))
		os.Exit(1)
	}
}

// subdirectories returns the case-insensitively sorted names of the non-hidden
// immediate subdirectories of root, including symlinks to directories.
func subdirectories(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Slice(dirs, func(i, j int) bool {
		a, b := strings.ToLower(dirs[i]), strings.ToLower(dirs[j])
		if a != b {
			return a < b
		}
		return dirs[i] < dirs[j]
	})
	return dirs, nil
}

// run executes the command in each directory concurrently and returns the
// results in the same order as dirs.
func run(ctx context.Context, dirs []string, name string, args []string) []*result {
	results := make([]*result, len(dirs))
	var wg sync.WaitGroup
	for i, dir := range dirs {
		r := &result{dir: dir}
		results[i] = r
		wg.Go(func() {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = dir
			cmd.Stdout = &r.output
			cmd.Stderr = &r.output
			r.err = cmd.Run()
			if errors.Is(r.err, exec.ErrNotFound) {
				r.err = fmt.Errorf("command not found: %s", name)
			}
		})
	}
	wg.Wait()
	return results
}
