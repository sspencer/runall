package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// configFile is the name of the config file read from the current directory
// when --conf is not given.
const configFile = ".runallrc"

// options holds the settings that control a run.
type options struct {
	sync     bool     // run one directory at a time, streaming output
	progress bool     // show a progress bar
	command  []string // program name followed by its arguments
}

// modeFlag is a boolean flag that sets the run mode. For --sync it sets sync
// to the flag's value; for --async (invert) it sets sync to the opposite.
type modeFlag struct {
	sync   *bool
	invert bool
}

func (m modeFlag) IsBoolFlag() bool { return true }

func (m modeFlag) String() string {
	if m.sync == nil {
		return "false"
	}
	return strconv.FormatBool(*m.sync != m.invert)
}

func (m modeFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return errors.New("must be true or false")
	}
	*m.sync = v != m.invert
	return nil
}

// resolveOptions determines the options for a run from the config file and
// the command-line arguments args, which take precedence. The config file is
// the one named by --conf if given (--conf= disables it), or else .runallrc in
// the current directory if it exists. It returns flag.ErrHelp if -h or --help
// was given.
func resolveOptions(args []string) (options, error) {
	// First pass: validate the command line and find --conf.
	conf, confSet, err := parseArgs(args, &options{})
	if err != nil {
		return options{}, err
	}

	var opts options
	switch {
	case !confSet:
		if err := loadConfig(configFile, &opts, false); err != nil {
			return options{}, err
		}
	case conf != "":
		if err := loadConfig(conf, &opts, true); err != nil {
			return options{}, err
		}
	}

	// Second pass: let the command line override the config file.
	if _, _, err := parseArgs(args, &opts); err != nil {
		return options{}, err
	}
	return opts, nil
}

// parseArgs parses runall's flags and command from args into opts. Only
// settings present in args are changed, so it can be applied first to the
// config file and then to the command line. It also returns the value of
// --conf and whether it was given. It returns flag.ErrHelp if -h or --help was
// given.
func parseArgs(args []string, opts *options) (conf string, confSet bool, err error) {
	fs := flag.NewFlagSet("runall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.Var(modeFlag{sync: &opts.sync}, "s", "")
	fs.Var(modeFlag{sync: &opts.sync}, "sync", "")
	fs.Var(modeFlag{sync: &opts.sync, invert: true}, "async", "")
	fs.BoolVar(&opts.progress, "p", opts.progress, "")
	fs.BoolVar(&opts.progress, "progress", opts.progress, "")
	var command string
	fs.StringVar(&command, "command", "", "")
	fs.StringVar(&conf, "conf", "", "")
	if err := fs.Parse(args); err != nil {
		return "", false, err
	}

	var commandSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "command":
			commandSet = true
		case "conf":
			confSet = true
		}
	})

	rest := fs.Args()
	switch {
	case commandSet && len(rest) > 0:
		return "", false, fmt.Errorf("--command cannot be combined with a command given as arguments (%q)", strings.Join(rest, " "))
	case commandSet:
		words, err := splitWords(command)
		if err != nil {
			return "", false, fmt.Errorf("--command: %w", err)
		}
		if len(words) == 0 {
			return "", false, errors.New("--command: empty command")
		}
		opts.command = words
	case len(rest) > 0:
		opts.command = rest
	}
	return conf, confSet, nil
}

// loadConfig applies the settings in the config file at path to opts. If the
// file doesn't exist, that is an error only if required is true. Any --conf
// in the file is ignored.
func loadConfig(path string, opts *options, required bool) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil
	}
	if err != nil {
		return err
	}
	if _, _, err := parseArgs(configArgs(string(data)), opts); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			err = errors.New("-h/--help is not allowed")
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// configArgs returns the arguments in a config file's contents. As with
// ripgrep's config file, each line is a single argument, taken literally
// apart from surrounding whitespace; blank lines and lines whose first
// non-blank character is '#' are skipped.
func configArgs(data string) []string {
	var args []string
	for line := range strings.Lines(data) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		args = append(args, line)
	}
	return args
}

// splitWords splits s into words the way a POSIX shell would, honoring single
// quotes, double quotes, and backslash escapes.
func splitWords(s string) ([]string, error) {
	var (
		words   []string
		word    strings.Builder
		inWord  bool
		quote   rune // 0, '\'' or '"'
		escaped bool
	)
	for _, c := range s {
		switch {
		case escaped:
			// Inside double quotes, a backslash only escapes a few characters.
			if quote == '"' && !strings.ContainsRune(`"\$`+"`", c) {
				word.WriteRune('\\')
			}
			if c != '\n' || quote != 0 { // unquoted backslash-newline is a line continuation
				word.WriteRune(c)
				inWord = true
			}
			escaped = false
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				word.WriteRune(c)
			}
		case quote == '"':
			switch c {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				word.WriteRune(c)
			}
		case c == '\\':
			escaped = true
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if escaped {
		return nil, errors.New("trailing backslash")
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}
