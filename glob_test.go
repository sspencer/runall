package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustGlobs(t *testing.T, raw ...string) []globRule {
	t.Helper()
	rules := make([]globRule, len(raw))
	for i, s := range raw {
		r, err := parseGlob(s)
		if err != nil {
			t.Fatalf("parseGlob(%q): %v", s, err)
		}
		rules[i] = r
	}
	return rules
}

func TestParseGlob(t *testing.T) {
	tests := []struct {
		raw      string
		negated  bool
		patterns []string
	}{
		{"archive", false, []string{"archive"}},
		{"archive/", false, []string{"archive"}},
		{"!archive/", true, []string{"archive"}},
		{"!/archive/", true, []string{"archive"}},
		{"!./archive/", true, []string{"archive"}},
		{"!**/archive/", true, []string{"archive"}},
		{"!./**/archive/", true, []string{"archive"}},
		{"**/", false, []string{"**"}},
		{`\!bang`, false, []string{"!bang"}},
		{"!!bang", true, []string{"!bang"}},
		{"[!0-9]*", false, []string{"[^0-9]*"}},
		{"[^0-9]*", false, []string{"[^0-9]*"}},
		{"[a!b]", false, []string{"[a!b]"}},
		{`\[!a]`, false, []string{`\[!a]`}},
		{"!{archive,ctest}/", true, []string{"archive", "ctest"}},
		{"!{archive/,ctest/}", true, []string{"archive", "ctest"}},
		{"{api,web}-*", false, []string{"api-*", "web-*"}},
		{"{a,{b,c}}", false, []string{"a", "b", "c"}},
		{"{a,b}-{1,2}", false, []string{"a-1", "a-2", "b-1", "b-2"}},
		{"app{,-dev}", false, []string{"app", "app-dev"}},
		{"[{,}]", false, []string{"[{,}]"}},
		{`\{a,b\}`, false, []string{`\{a,b\}`}},
		{"!a#b", true, []string{"a#b"}},
	}

	for _, tt := range tests {
		got, err := parseGlob(tt.raw)
		if err != nil {
			t.Errorf("parseGlob(%q): unexpected error: %v", tt.raw, err)
			continue
		}
		if got.negated != tt.negated || !reflect.DeepEqual(got.patterns, tt.patterns) {
			t.Errorf("parseGlob(%q) = (negated=%v, patterns=%q), want (negated=%v, patterns=%q)",
				tt.raw, got.negated, got.patterns, tt.negated, tt.patterns)
		}
	}

	bad := []string{
		"",
		"!",
		"/",
		"!/",
		"./",
		"!./",
		"[abc",
		"{a,b",
		"a,b}",
		"{a,b}}",
		"{a,}",
		strings.Repeat("{a,b}", 11),
	}
	for _, raw := range bad {
		if _, err := parseGlob(raw); err == nil {
			t.Errorf("parseGlob(%q): expected error", raw)
		}
	}
}

func TestMatchGlobs(t *testing.T) {
	allDirs := []string{".git", ".github", "api", "archive", "ctest", "service-a", "service-b", "Web", "!bang", "a#b"}

	tests := []struct {
		name  string
		globs []string
		want  []string
	}{
		{
			name:  "no globs excludes hidden only",
			globs: nil,
			want:  []string{"api", "archive", "ctest", "service-a", "service-b", "Web", "!bang", "a#b"},
		},
		{
			name:  "exclude multiple directories with trailing slash",
			globs: []string{"!archive/", "!ctest/"},
			want:  []string{"api", "service-a", "service-b", "Web", "!bang", "a#b"},
		},
		{
			name:  "exclude with brace alternation",
			globs: []string{"!{archive,ctest}/"},
			want:  []string{"api", "service-a", "service-b", "Web", "!bang", "a#b"},
		},
		{
			name:  "positive globs only",
			globs: []string{"api", "service-*"},
			want:  []string{"api", "service-a", "service-b"},
		},
		{
			name:  "positive then negative",
			globs: []string{"service-*", "!service-b/"},
			want:  []string{"service-a"},
		},
		{
			name:  "later positive overrides earlier negative",
			globs: []string{"!archive/", "!ctest/", "archive/"},
			want:  []string{"archive"},
		},
		{
			name:  "star overrides earlier negatives without matching hidden dirs",
			globs: []string{"!archive/", "!ctest/", "*"},
			want:  []string{"api", "archive", "ctest", "service-a", "service-b", "Web", "!bang", "a#b"},
		},
		{
			name:  "include all exclude prefix re-include specific",
			globs: []string{"*", "!service-*", "service-a"},
			want:  []string{"api", "archive", "ctest", "service-a", "Web", "!bang", "a#b"},
		},
		{
			name:  "explicit hidden directory",
			globs: []string{".github"},
			want:  []string{".github"},
		},
		{
			name:  "dot wildcard with hidden exclusion",
			globs: []string{".*", "!.git/"},
			want:  []string{".github"},
		},
		{
			name:  "non-hidden plus explicit hidden directory",
			globs: []string{"api", ".github"},
			want:  []string{".github", "api"},
		},
		{
			name:  "negative star excludes previously included hidden dir",
			globs: []string{".github", "!*"},
			want:  nil,
		},
		{
			name:  "escaped leading exclamation matches literal bang dir",
			globs: []string{`\!bang`},
			want:  []string{"!bang"},
		},
		{
			name:  "double leading exclamation excludes literal bang dir",
			globs: []string{"!!bang"},
			want:  []string{"api", "archive", "ctest", "service-a", "service-b", "Web", "a#b"},
		},
		{
			name:  "character class negation",
			globs: []string{"service-[!b]"},
			want:  []string{"service-a"},
		},
		{
			name:  "hash in directory name",
			globs: []string{"!a#b"},
			want:  []string{"api", "archive", "ctest", "service-a", "service-b", "Web", "!bang"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := mustGlobs(t, tt.globs...)
			var got []string
			for _, d := range allDirs {
				if matchGlobs(d, rules) {
					got = append(got, d)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("matchGlobs(%q) = %q, want %q", tt.globs, got, tt.want)
			}
		})
	}
}

func TestSubdirectories(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{".git", ".github", "api", "archive", "ctest", "service-a", "service-b", "Web"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"README.md", "service-file"} {
		writeFile(t, filepath.Join(root, f), "")
	}
	if err := os.Symlink(filepath.Join(root, "api"), filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "README.md"), filepath.Join(root, "link-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "link-broken")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		globs []string
		want  []string
	}{
		{
			name:  "default no globs",
			globs: nil,
			want:  []string{"api", "archive", "ctest", "link-dir", "service-a", "service-b", "Web"},
		},
		{
			name:  "ignore archive and ctest",
			globs: []string{"!archive/", "!ctest/"},
			want:  []string{"api", "link-dir", "service-a", "service-b", "Web"},
		},
		{
			name:  "match prefix skips regular files",
			globs: []string{"service-*"},
			want:  []string{"service-a", "service-b"},
		},
		{
			name:  "match symlinks keeps directory symlinks only",
			globs: []string{"link-*"},
			want:  []string{"link-dir"},
		},
		{
			name:  "explicit hidden dir",
			globs: []string{".github/"},
			want:  []string{".github"},
		},
		{
			name:  "nothing matches",
			globs: []string{"nonexistent*"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := subdirectories(root, mustGlobs(t, tt.globs...))
			if err != nil {
				t.Fatalf("subdirectories: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subdirectories(%q) = %q, want %q", tt.globs, got, tt.want)
			}
		})
	}
}
