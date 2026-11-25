package main

import (
	"errors"
	"path"
	"strings"
)

// maxGlobExpansions bounds the number of patterns produced by expanding brace
// alternations in a single --glob argument.
const maxGlobExpansions = 1024

// globRule is a compiled -g/--glob directory filter.
type globRule struct {
	raw      string   // original pattern as supplied by the user
	negated  bool     // true when prefixed with '!'
	patterns []string // normalized path.Match patterns after brace expansion
}

// parseGlob compiles a -g/--glob pattern. A leading '!' excludes matching
// directories; '\!' matches a literal leading '!'. Leading '/', './', '**/'
// and trailing '/' are stripped so gitignore/ripgrep forms such as "!archive/"
// and "!**/archive/" match immediate subdirectories. Patterns support '*',
// '?', '[...]' (with '[!...]' or '[^...]'), and '{a,b}' brace alternation.
func parseGlob(raw string) (globRule, error) {
	if raw == "" {
		return globRule{}, errors.New("empty pattern")
	}

	var negated bool
	body := raw
	switch {
	case strings.HasPrefix(body, "!"):
		negated = true
		body = body[1:]
	case strings.HasPrefix(body, `\!`):
		body = body[1:] // literal leading '!'
	}
	if body == "" {
		return globRule{}, errors.New("empty pattern")
	}

	expanded, err := expandBraces(body, maxGlobExpansions)
	if err != nil {
		return globRule{}, err
	}

	patterns := make([]string, 0, len(expanded))
	for _, p := range expanded {
		p = normalizeGlobPattern(p)
		if p == "" {
			return globRule{}, errors.New("empty pattern")
		}
		if _, err := path.Match(p, ""); err != nil {
			return globRule{}, err
		}
		patterns = append(patterns, p)
	}

	return globRule{
		raw:      raw,
		negated:  negated,
		patterns: patterns,
	}, nil
}

// normalizeGlobPattern strips directory-style prefixes/suffixes ('/', './',
// '**/', trailing '/') and converts '[!...]' character-class negation into
// path.Match's '[^...]' syntax.
func normalizeGlobPattern(p string) string {
	for {
		switch {
		case strings.HasPrefix(p, "./"):
			p = p[2:]
		case strings.HasPrefix(p, "/"):
			p = p[1:]
		case strings.HasPrefix(p, "**/") && len(p) > 3:
			p = p[3:]
		default:
			p = strings.TrimRight(p, "/")
			return translateCharClassNegation(p)
		}
	}
}

// translateCharClassNegation rewrites '[!...]' to '[^...]' so both standard
// glob/gitignore negation and Go path.Match negation work.
func translateCharClassNegation(p string) string {
	var b strings.Builder
	b.Grow(len(p))
	escaped := false
	inClass := false
	classStart := false
	for _, r := range p {
		if escaped {
			b.WriteRune(r)
			escaped = false
			classStart = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			classStart = false
			continue
		}
		if inClass {
			if classStart && r == '!' {
				b.WriteRune('^')
				classStart = false
				continue
			}
			if r == ']' {
				inClass = false
			}
			classStart = false
			b.WriteRune(r)
			continue
		}
		if r == '[' {
			inClass = true
			classStart = true
		}
		b.WriteRune(r)
	}
	return b.String()
}

// expandBraces expands '{a,b}' alternations (including nested braces) into a
// flat list of patterns, ignoring braces that are backslash-escaped or inside
// '[...]' character classes.
func expandBraces(s string, limit int) ([]string, error) {
	openIdx := -1
	escaped := false
	inClass := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			continue
		}
		switch c {
		case '[':
			inClass = true
		case '{':
			openIdx = i
		case '}':
			return nil, errors.New("unexpected '}'")
		}
		if openIdx >= 0 {
			break
		}
	}
	if openIdx < 0 {
		return []string{s}, nil
	}

	// Scan for comma-separated alternatives and the matching closing brace.
	var alts []string
	depth := 1
	partStart := openIdx + 1
	closeIdx := -1
	escaped = false
	inClass = false
	for i := openIdx + 1; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if inClass {
			if c == ']' {
				inClass = false
			}
			continue
		}
		switch c {
		case '[':
			inClass = true
		case '{':
			depth++
		case ',':
			if depth == 1 {
				alts = append(alts, s[partStart:i])
				partStart = i + 1
			}
		case '}':
			depth--
			if depth == 0 {
				alts = append(alts, s[partStart:i])
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return nil, errors.New("unclosed '{'")
	}

	prefix := s[:openIdx]
	suffixes, err := expandBraces(s[closeIdx+1:], limit)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, alt := range alts {
		expandedAlts, err := expandBraces(alt, limit)
		if err != nil {
			return nil, err
		}
		for _, ea := range expandedAlts {
			for _, suf := range suffixes {
				if len(out) >= limit {
					return nil, errors.New("too many brace expansions")
				}
				out = append(out, prefix+ea+suf)
			}
		}
	}
	return out, nil
}

// matches reports whether directory name matches this rule. For hidden
// directories (starting with '.'), a positive rule only matches if the
// normalized pattern explicitly begins with '.' or '\.'.
func (g globRule) matches(name string) bool {
	hidden := strings.HasPrefix(name, ".")
	for _, p := range g.patterns {
		if hidden && !g.negated && !patternStartsWithDot(p) {
			continue
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

func patternStartsWithDot(p string) bool {
	return strings.HasPrefix(p, ".") || strings.HasPrefix(p, `\.`)
}

// matchGlobs reports whether a directory named name should be included given
// the ordered list of glob rules.
//
// With no rules, every non-hidden directory is included.
// With only negative rules, every non-hidden directory is included unless
// excluded by a rule.
// With one or more positive rules, only directories matched by a positive rule
// (and not excluded by a later negative rule) are included.
// When multiple rules match, the last matching rule wins.
func matchGlobs(name string, globs []globRule) bool {
	hidden := strings.HasPrefix(name, ".")
	if len(globs) == 0 {
		return !hidden
	}

	hasPositive := false
	for _, g := range globs {
		if !g.negated {
			hasPositive = true
			break
		}
	}

	included := !hidden && !hasPositive
	for _, g := range globs {
		if g.matches(name) {
			included = !g.negated
		}
	}
	return included
}
