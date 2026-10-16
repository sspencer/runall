# runall

Run a command in every immediate subdirectory of the current directory, in parallel, and print the results in alphabetical order once they all finish.

```
$ runall git pull
==> api <==
Already up to date.

==> Web <==
Updating 3f2a1c4..9b8e7d2
Fast-forward
 README.md | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

==> worker <==
Already up to date.
```

## Install

Requires Go 1.25 or later. From the project directory:

```
go install
```

This puts `runall` in `$(go env GOPATH)/bin`, so make sure that directory is on your `PATH`.

Or build a local binary:

```
go build -o runall .
```

## Usage

```
runall <command> [args...]
```

Run `runall` with no arguments (or `runall -h` / `--help`) to print usage.

Examples:

```
runall git pull
runall make test
runall git status --short
runall ls -la
runall sh -c 'git log -1 --oneline | cat'
```

## Behavior

- **Directories:** runs in each immediate (non-recursive) subdirectory, including symlinks that point to directories. Hidden directories (names starting with `.`) are skipped.
- **Parallel execution:** all commands start at once, so the total run time is about as long as the slowest directory.
- **Buffered output:** each directory's stdout and stderr are captured together and printed under a `==> dirname <==` header after every command has finished.
- **Ordering:** sections are sorted alphabetically, ignoring case.
- **No shell:** arguments are passed straight to the program. To use pipes, globs, or other shell features, call a shell yourself: `runall sh -c '...'`.
- **Errors:** a failed command is noted under its directory's section, and a summary such as `runall: 1 of 3 commands failed` is printed to stderr.
- **Interrupts:** Ctrl-C (SIGINT) or SIGTERM stops all running commands.

## Exit status

| Code | Meaning                                    |
|------|--------------------------------------------|
| 0    | Every command succeeded (or there were no subdirectories) |
| 1    | One or more commands failed, or the current directory couldn't be read |
| 2    | No command was given                       |
