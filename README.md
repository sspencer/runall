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
make install
```

This builds `./runall` and installs it in your Go bin directory (`$(go env GOBIN)`, or `$(go env GOPATH)/bin` if `GOBIN` is unset), so make sure that directory is on your `PATH`.

Other targets:

| Target           | Action                                  |
|------------------|-----------------------------------------|
| `make`           | Same as `make install`                  |
| `make build`     | Build a local `./runall` binary only    |
| `make uninstall` | Remove the installed binary             |
| `make vet`       | Run `go vet`                            |
| `make clean`     | Remove the local binary                 |

## Usage

```
runall [options] <command> [args...]
runall [options] --command=<command> [args...]
```

| Option | Meaning |
|--------|---------|
| `-s`, `--sync[=true\|false]` | Run synchronously: one directory at a time, in alphabetical order, with output streamed directly to the terminal. Defaults to `false`. |
| `--async[=true\|false]` | Run in parallel (the default). `--async=false` is the same as `--sync`. If `--sync` and `--async` are both given, the last one wins. |
| `-p`, `--progress[=true\|false]` | Show a progress bar on stderr that counts subdirectories completed out of the total. Ignored when stderr isn't a terminal. Defaults to `false`. |
| `--command=<cmd>` | The command to run, as a single string (split using shell-style quoting). Use instead of giving the command as trailing arguments; using both is an error. |
| `--conf=<file>` | Read options from `<file>` instead of `./.runallrc`. The file must exist. `--conf=` (empty) reads no config file at all. |
| `-h`, `--help` | Print usage |

Options must come before the command; everything from the first non-option argument on is the command. Use `--` to end options explicitly. Run `runall` with no arguments to print usage.

Examples:

```
runall git pull
runall make test
runall git status --short
runall ls -la
runall -s git pull
runall --async git status
runall -p git pull
runall --command='git log -1 --oneline'
runall --conf=../pull.runallrc
runall sh -c 'git log -1 --oneline | cat'
```

## Config file (`.runallrc`)

Unless `--conf` is given, runall looks for a `.runallrc` file in the current directory and, if it exists, reads options from it before the command line. The format follows [ripgrep's config file](https://github.com/BurntSushi/ripgrep/blob/master/GUIDE.md#configuration-file):

- Each line is a single argument, taken exactly as written apart from leading and trailing whitespace. No quoting is needed, so write `--command=git pull --rebase`. Within the `--command` value, shell-style quotes still group words, e.g. `--command=sh -c 'git log -1 | cat'`.
- Blank lines and lines whose first non-blank character is `#` are ignored. There are no trailing comments, so a `#` later in a line is kept.
- `--conf` is ignored inside a config file.

```
# .runallrc
# Stream output, one repo at a time.
--sync
--command=git pull --rebase
```

With that file in place, a bare `runall` runs `git pull --rebase` in each subdirectory one at a time. Command-line arguments override the file: `runall --async git status` runs `git status` in parallel.

## Behavior

- **Directories:** runs in each immediate (non-recursive) subdirectory, including symlinks that point to directories. Hidden directories (names starting with `.`) are skipped.
- **Parallel execution:** all commands start at once, so the total run time is about as long as the slowest directory.
- **Buffered output:** each directory's stdout and stderr are captured together and printed under a `==> dirname <==` header after every command has finished.
- **Synchronous mode (`-s` / `--sync`):** commands run one at a time in alphabetical order. Each directory's header is printed first, and the command is connected directly to the terminal (stdin, stdout, and stderr), so live progress, colors, and interactive prompts work. Ctrl-C stops the current command and skips the rest.
- **Progress bar (`-p` / `--progress`):** the total is the number of subdirectories, and the bar advances as each directory's command finishes. It's drawn on stderr using [schollz/progressbar](https://github.com/schollz/progressbar). In parallel mode it updates while commands run and is erased before the results are printed. In synchronous mode it's shown between directories and erased while each command runs, so it doesn't mix with the command's output.
- **Ordering:** sections are sorted alphabetically, ignoring case.
- **No shell:** arguments are passed straight to the program. To use pipes, globs, or other shell features, call a shell yourself: `runall sh -c '...'`.
- **Errors:** a failed command is noted under its directory's section, and a summary such as `runall: 1 of 3 commands failed` is printed to stderr.
- **Interrupts:** Ctrl-C (SIGINT) or SIGTERM stops all running commands.

## Exit status

| Code | Meaning                                    |
|------|--------------------------------------------|
| 0    | Every command succeeded (or there were no subdirectories) |
| 1    | One or more commands failed, or the current directory couldn't be read |
| 2    | No command was given, the options or config file were invalid, or the `--conf` file couldn't be read |
