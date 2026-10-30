// Command runall runs the given command in every immediate subdirectory of the
// current directory, in parallel, and prints each directory's buffered output
// in alphabetical order once all commands have finished. With -s, it instead
// runs the command in one directory at a time, streaming output as it goes.
//
// Usage:
//
//	runall git pull
//	runall ls -la
//	runall -s git pull
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

const usage = `Usage: runall [-s] <command> [args...]

Runs the specified command in each immediate subdirectory, in parallel, and
prints each directory's output in alphabetical order once all have finished.

Options:
  -s  Run synchronously: one directory at a time, in alphabetical order,
      with output streamed directly to the terminal.

Examples:
  runall git pull
  runall make test
  runall ls -la
  runall -s git pull
  runall sh -c 'git log -1 --oneline | cat'
`

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(usage)
		return
	}
	sequential := len(args) > 0 && args[0] == "-s"
	if sequential {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
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

	var failed, total int
	if sequential {
		failed, total = runSync(ctx, dirs, args[0], args[1:])
	} else {
		results := run(ctx, dirs, args[0], args[1:])
		total = len(results)
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
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\nrunall: %d of %d commands failed\n", failed, total)
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

// runSync executes the command in each directory one at a time, connecting it
// directly to the terminal. It stops early if ctx is cancelled and returns the
// number of commands that failed and the number that were run.
func runSync(ctx context.Context, dirs []string, name string, args []string) (failed, total int) {
	for i, dir := range dirs {
		if ctx.Err() != nil {
			break
		}
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("==> %s <==\n", dir)
		total++
		if err := runOne(ctx, dir, name, args, os.Stdin, os.Stdout, os.Stderr); err != nil {
			failed++
			fmt.Printf("runall: %s: %v\n", dir, err)
		}
	}
	return failed, total
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
			r.err = runOne(ctx, dir, name, args, nil, &r.output, &r.output)
		})
	}
	wg.Wait()
	return results
}

// runOne runs the command in dir with the given standard streams.
func runOne(ctx context.Context, dir, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		err = fmt.Errorf("command not found: %s", name)
	}
	return err
}
