package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"git pull", []string{"git", "pull"}},
		{"  a\t b\n c  ", []string{"a", "b", "c"}},
		{`sh -c 'echo "hi" | cat'`, []string{"sh", "-c", `echo "hi" | cat`}},
		{`a "b c" d\ e`, []string{"a", "b c", "d e"}},
		{`"a\"b" "\n"`, []string{`a"b`, `\n`}},
		{`''`, []string{""}},
		{"echo #1", []string{"echo", "#1"}},
		{"a \\\n b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		got, err := splitWords(tt.in)
		if err != nil {
			t.Errorf("splitWords(%q): %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitWords(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	for _, in := range []string{`'abc`, `"abc`, `abc\`} {
		if _, err := splitWords(in); err == nil {
			t.Errorf("splitWords(%q): expected error", in)
		}
	}
}

func TestConfigArgs(t *testing.T) {
	data := "# comment\n\n  --sync  \n--command=git pull --rebase\n   # indented comment\n--glob=!a#b\r\n"
	want := []string{"--sync", "--command=git pull --rebase", "--glob=!a#b"}
	if got := configArgs(data); !reflect.DeepEqual(got, want) {
		t.Errorf("configArgs = %q, want %q", got, want)
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args []string
		want options
	}{
		{[]string{"git", "pull"}, options{command: []string{"git", "pull"}}},
		{[]string{"-s", "git", "pull"}, options{sync: true, command: []string{"git", "pull"}}},
		{[]string{"--sync", "ls"}, options{sync: true, command: []string{"ls"}}},
		{[]string{"--sync=true", "ls"}, options{sync: true, command: []string{"ls"}}},
		{[]string{"--sync=false", "ls"}, options{command: []string{"ls"}}},
		{[]string{"--async", "ls"}, options{command: []string{"ls"}}},
		{[]string{"--async=false", "ls"}, options{sync: true, command: []string{"ls"}}},
		{[]string{"--sync", "--async", "ls"}, options{command: []string{"ls"}}},
		{[]string{"--async", "-s", "ls"}, options{sync: true, command: []string{"ls"}}},
		{[]string{"--command", "git log -1", "-s"}, options{sync: true, command: []string{"git", "log", "-1"}}},
		{[]string{"--command=git pull --rebase"}, options{command: []string{"git", "pull", "--rebase"}}},
		{[]string{"--command=sh -c 'echo a | cat'"}, options{command: []string{"sh", "-c", "echo a | cat"}}},
		{[]string{"ls", "-s"}, options{command: []string{"ls", "-s"}}},
		{[]string{"-p", "ls"}, options{progress: true, command: []string{"ls"}}},
		{[]string{"--progress", "-s", "ls"}, options{sync: true, progress: true, command: []string{"ls"}}},
		{[]string{"-p", "--progress=false", "ls"}, options{command: []string{"ls"}}},
		{[]string{"--glob=!archive/", "--glob=!ctest/", "ls"}, options{globs: mustGlobs(t, "!archive/", "!ctest/"), command: []string{"ls"}}},
		{[]string{"-g", "api*", "-g=!api-old/", "ls"}, options{globs: mustGlobs(t, "api*", "!api-old/"), command: []string{"ls"}}},
		{[]string{"--", "-weird"}, options{command: []string{"-weird"}}},
		{nil, options{}},
	}
	for _, tt := range tests {
		var got options
		if _, _, err := parseArgs(tt.args, &got); err != nil {
			t.Errorf("parseArgs(%q): %v", tt.args, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
		}
	}

	for _, args := range [][]string{
		{"--command", "ls", "pwd"},
		{"--command", ""},
		{"--sync=maybe", "ls"},
		{"--async=maybe", "ls"},
		{"--glob=", "ls"},
		{"--glob=!", "ls"},
		{"--glob=[bad", "ls"},
		{"--glob={a,b", "ls"},
		{"-g", "", "ls"},
		{"--bogus", "ls"},
	} {
		var o options
		if _, _, err := parseArgs(args, &o); err == nil {
			t.Errorf("parseArgs(%q): expected error", args)
		}
	}

	for _, h := range []string{"-h", "--help"} {
		var o options
		if _, _, err := parseArgs([]string{h}, &o); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("parseArgs(%q) = %v, want flag.ErrHelp", h, err)
		}
	}

	for _, tt := range []struct {
		args    []string
		conf    string
		confSet bool
	}{
		{[]string{"ls"}, "", false},
		{[]string{"--conf=x.rc", "ls"}, "x.rc", true},
		{[]string{"--conf", "x.rc"}, "x.rc", true},
		{[]string{"--conf=", "ls"}, "", true},
	} {
		conf, confSet, err := parseArgs(tt.args, &options{})
		if err != nil || conf != tt.conf || confSet != tt.confSet {
			t.Errorf("parseArgs(%q) conf = %q, %v, %v; want %q, %v", tt.args, conf, confSet, err, tt.conf, tt.confSet)
		}
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOptions(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	resolve := func(args ...string) options {
		t.Helper()
		o, err := resolveOptions(args)
		if err != nil {
			t.Fatalf("resolveOptions(%q): %v", args, err)
		}
		return o
	}

	// No config file: defaults.
	if got := resolve("ls"); !reflect.DeepEqual(got, options{command: []string{"ls"}}) {
		t.Errorf("no config: %+v", got)
	}

	writeFile(t, configFile, "# defaults\n--sync\n--progress\n--command=git pull --rebase\n--conf=ignored.rc\n")
	other := filepath.Join(dir, "other.rc")
	writeFile(t, other, "--command=make test\n")

	// .runallrc is used as is when there are no arguments.
	if got, want := resolve(), (options{sync: true, progress: true, command: []string{"git", "pull", "--rebase"}}); !reflect.DeepEqual(got, want) {
		t.Errorf(".runallrc: got %+v, want %+v", got, want)
	}
	// The command line overrides it.
	if got, want := resolve("--async", "--progress=false", "ls"), (options{command: []string{"ls"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("override: got %+v, want %+v", got, want)
	}
	// --conf replaces .runallrc entirely.
	if got, want := resolve("--conf="+other), (options{command: []string{"make", "test"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("--conf: got %+v, want %+v", got, want)
	}
	// --conf= disables config files.
	if got, want := resolve("--conf=", "ls"), (options{command: []string{"ls"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("--conf=: got %+v, want %+v", got, want)
	}

	// Globs from .runallrc are preserved and CLI globs are appended after them.
	writeFile(t, configFile, "# IGNORED DIRECTORIES\n--glob=!archive/\n--glob=!ctest/\n--command=git status\n")
	if got, want := resolve(), (options{
		globs:   mustGlobs(t, "!archive/", "!ctest/"),
		command: []string{"git", "status"},
	}); !reflect.DeepEqual(got, want) {
		t.Errorf(".runallrc globs: got %+v, want %+v", got, want)
	}
	if got, want := resolve("--glob=!tmp/", "-g", "archive/", "ls"), (options{
		globs:   mustGlobs(t, "!archive/", "!ctest/", "!tmp/", "archive/"),
		command: []string{"ls"},
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("merged globs: got %+v, want %+v", got, want)
	}
	if got, want := resolve("--conf=", "--glob=api*", "ls"), (options{
		globs:   mustGlobs(t, "api*"),
		command: []string{"ls"},
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("--conf= clears config globs: got %+v, want %+v", got, want)
	}

	// Errors: missing --conf file, bad config contents.
	if _, err := resolveOptions([]string{"--conf=missing.rc", "ls"}); err == nil {
		t.Error("expected error for missing --conf file")
	}
	writeFile(t, configFile, "--glob=[bad\n")
	if _, err := resolveOptions([]string{"ls"}); err == nil {
		t.Error("expected error for invalid --glob in .runallrc")
	}
	writeFile(t, configFile, "--sync=maybe\n")
	if _, err := resolveOptions([]string{"ls"}); err == nil {
		t.Error("expected error for bad .runallrc")
	}
	writeFile(t, configFile, "--help\n")
	if _, err := resolveOptions([]string{"ls"}); err == nil || errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help in config: got %v, want non-help error", err)
	}
	// --help on the command line wins without reading the config.
	if _, err := resolveOptions([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help: got %v, want flag.ErrHelp", err)
	}
}
