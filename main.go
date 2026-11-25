// Command runall runs the given command in every immediate subdirectory of the
// current directory, in parallel, and prints each directory's buffered output
// in alphabetical order once all commands have finished. With -s/--sync, it
// instead runs the command in one directory at a time, streaming output as it
// goes. Default options and the command can be set in a config file (.runallrc
// in the current directory, or the file named by --conf); command-line
// arguments override it.
//
// Usage:
//
//	runall git pull
//	runall ls -la
//	runall -s git pull
//	runall --sync --command='git pull'
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
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

	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

type result struct {
	dir    string
	output bytes.Buffer
	err    error
}

const usage = `Usage: runall [options] <command> [args...]
       runall [options] --command=<command> [args...]

Runs the specified command in each immediate subdirectory, in parallel, and
prints each directory's output in alphabetical order once all have finished.

Options:
  -s, --sync[=true|false]   Run synchronously: one directory at a time, in
                            alphabetical order, with output streamed directly
                            to the terminal. Default: false.
  --async[=true|false]      Run in parallel (the default). --async=false is
                            the same as --sync. If both are given, the last
                            one wins.
  --command=<cmd>           The command to run, as a single string. Use
                            instead of giving the command as arguments.
  -p, --progress[=true|false]
                            Show a progress bar on stderr counting the
                            directories completed. Ignored if stderr is not
                            a terminal. Default: false.
  -g, --glob=<pattern>      Include or exclude subdirectories matching
                            <pattern>. Prefix with '!' to exclude. May be
                            repeated; later globs take precedence. Trailing
                            '/' is optional.
  --conf=<file>             Read options from <file> instead of ./.runallrc.
                            --conf= (empty) reads no config file at all.
  -h, --help                Show this help.

Config file:
  Unless --conf is given, options are read from .runallrc in the current
  directory, if it exists, before the command line, which overrides them.
  Each line holds one argument, exactly as written (no quoting needed).
  Blank lines and lines starting with '#' are ignored. --conf is ignored in
  a config file. For example:

    # .runallrc
    --sync
    --command=git pull --rebase
    --glob=!archive/
    --glob=!ctest/

Placeholders:
  In both positional commands and --command, '{}' is replaced with the
  subdirectory name and '{path}' with its absolute path. Use '{{}}' or
  '{{path}}' for literal braces.

Examples:
  runall git pull
  runall make test
  runall ls -la
  runall -s git pull
  runall --async git status
  runall -p git pull
  runall --glob='!archive/' --glob='!ctest/' git pull
  runall -g 'api-*' git status
  runall echo {} is at {path}
  runall --command='git log -1 --oneline'
  runall --conf=../pull.runallrc git status
  runall sh -c 'git log -1 --oneline | cat'
`

func main() {
	opts, err := resolveOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "runall: %v\n\nRun 'runall --help' for usage.\n", err)
		os.Exit(2)
	}
	if len(opts.command) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	args := opts.command

	dirs, err := subdirectories(".", opts.globs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runall: %v\n", err)
		os.Exit(1)
	}
	if len(dirs) == 0 {
		if len(opts.globs) > 0 {
			fmt.Fprintln(os.Stderr, "runall: no matching subdirectories")
		} else {
			fmt.Fprintln(os.Stderr, "runall: no subdirectories found")
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var bar *progressbar.ProgressBar
	// The bar redraws itself in place, which only makes sense on a terminal.
	if opts.progress && term.IsTerminal(int(os.Stderr.Fd())) {
		bar = newProgressBar(len(dirs))
	}

	var failed, total int
	if opts.sync {
		failed, total = runSync(ctx, dirs, args[0], args[1:], bar)
		if bar != nil {
			bar.Clear() // in case it was interrupted before finishing
		}
	} else {
		results := run(ctx, dirs, args[0], args[1:], bar)
		if bar != nil {
			bar.Clear()
		}
		total = len(results)
		for i, r := range results {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("==> %s <==\n", r.dir)
			_, _ = os.Stdout.Write(r.output.Bytes())
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

// subdirectories returns the case-insensitively sorted names of the immediate
// subdirectories of root (including symlinks to directories) that match globs.
// Hidden directories (starting with '.') are excluded unless explicitly matched
// by a positive glob starting with '.'.
func subdirectories(root string, globs []globRule) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if !matchGlobs(name, globs) {
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

// newProgressBar returns a progress bar on stderr that counts n directories.
// It erases itself once all n are done.
func newProgressBar(n int) *progressbar.ProgressBar {
	return progressbar.NewOptions(n,
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription("runall"),
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionSetElapsedTime(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionClearOnFinish(),
	)
}

// runSync executes the command in each directory one at a time, connecting it
// directly to the terminal. If bar is non-nil, it is advanced after each
// directory and erased while a command runs so it doesn't mix with the
// command's output. It stops early if ctx is cancelled and returns the number
// of commands that failed and the number that were run.
func runSync(ctx context.Context, dirs []string, name string, args []string, bar *progressbar.ProgressBar) (failed, total int) {
	for i, dir := range dirs {
		if ctx.Err() != nil {
			break
		}
		if bar != nil {
			bar.Clear()
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
		if bar != nil {
			bar.Add(1)
		}
	}
	return failed, total
}

// run executes the command in each directory concurrently and returns the
// results in the same order as dirs. If bar is non-nil, it is advanced as each
// directory finishes.
func run(ctx context.Context, dirs []string, name string, args []string, bar *progressbar.ProgressBar) []*result {
	results := make([]*result, len(dirs))
	var wg sync.WaitGroup
	for i, dir := range dirs {
		r := &result{dir: dir}
		results[i] = r
		wg.Go(func() {
			r.err = runOne(ctx, dir, name, args, nil, &r.output, &r.output)
			if bar != nil {
				bar.Add(1)
			}
		})
	}
	wg.Wait()
	return results
}

// expandPlaceholders replaces '{}' with dirName and '{path}' with absPath in
// name and each element of args. '{{}}' and '{{path}}' escape to literal '{}'
// and '{path}'.
func expandPlaceholders(name string, args []string, dirName, absPath string) (string, []string) {
	r := strings.NewReplacer(
		"{{}}", "{}",
		"{{path}}", "{path}",
		"{}", dirName,
		"{path}", absPath,
	)
	expandedName := r.Replace(name)
	expandedArgs := make([]string, len(args))
	for i, a := range args {
		expandedArgs[i] = r.Replace(a)
	}
	return expandedName, expandedArgs
}

// runOne runs the command in dir with the given standard streams, substituting
// '{}' and '{path}' placeholders for the directory name and absolute path.
func runOne(ctx context.Context, dir, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	absPath, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	expandedName, expandedArgs := expandPlaceholders(name, args, filepath.Base(dir), absPath)
	cmd := exec.CommandContext(ctx, expandedName, expandedArgs...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		err = fmt.Errorf("command not found: %s", expandedName)
	}
	return err
}
