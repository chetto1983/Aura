package tokenjuice

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// LoadOptions configures which rule layers to load.
type LoadOptions struct {
	Builtin bool   // when true, include the embedded builtin rules
	UserDir string // path to a user-local rule directory; "" = skip (v1.1)
}

var cachedBuiltin = sync.OnceValue(func() []*CompiledRule {
	rules, err := loadFromFS(builtinFS, "builtin")
	if err != nil || len(rules) == 0 {
		slog.Default().Warn("tokenjuice: failed loading builtin rules; using synthetic fallback",
			"err", err)
		return syntheticFallback()
	}
	return rules
})

// LoadBuiltinRules returns the compiled builtin rule set, loaded once via
// sync.OnceValue. Safe to call concurrently.
func LoadBuiltinRules() []*CompiledRule {
	return cachedBuiltin()
}

// loadFromFS reads all *.json files in dir within fsys and returns compiled rules.
func loadFromFS(fsys fs.FS, dir string) ([]*CompiledRule, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("tokenjuice: readdir %q: %w", dir, err)
	}
	var rules []*CompiledRule
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := fs.ReadFile(fsys, dir+"/"+entry.Name())
		if err != nil {
			slog.Warn("tokenjuice: read rule failed", "file", entry.Name(), "err", err)
			continue
		}
		var r Rule
		if err := json.Unmarshal(data, &r); err != nil {
			slog.Warn("tokenjuice: parse rule failed", "file", entry.Name(), "err", err)
			continue
		}
		cr, err := compileRule(r, RuleOriginBuiltin, "builtin:"+r.ID)
		if err != nil {
			slog.Warn("tokenjuice: compile rule failed", "id", r.ID, "err", err)
			continue
		}
		rules = append(rules, cr)
	}
	return rules, nil
}

// compileRule pre-compiles all regex patterns in a Rule.
// Bad patterns are dropped with a warning (never fatal per Aura boot-non-fatal policy).
func compileRule(r Rule, origin RuleOrigin, path string) (*CompiledRule, error) {
	cr := &CompiledRule{Rule: r, Source: origin, Path: path}
	if r.Filters != nil {
		for _, p := range r.Filters.SkipPatterns {
			rx, err := compilePatternWithFlags(p.Pattern, p.Flags)
			if err != nil {
				slog.Warn("tokenjuice: skip pattern compile failed", "id", r.ID, "err", err)
				continue
			}
			cr.Compiled.SkipPatterns = append(cr.Compiled.SkipPatterns, rx)
		}
		for _, p := range r.Filters.KeepPatterns {
			rx, err := compilePatternWithFlags(p.Pattern, p.Flags)
			if err != nil {
				slog.Warn("tokenjuice: keep pattern compile failed", "id", r.ID, "err", err)
				continue
			}
			cr.Compiled.KeepPatterns = append(cr.Compiled.KeepPatterns, rx)
		}
	}
	for _, c := range r.Counters {
		rx, err := compilePatternWithFlags(c.Pattern, c.Flags)
		if err != nil {
			slog.Warn("tokenjuice: counter compile failed", "id", r.ID, "name", c.Name, "err", err)
			continue
		}
		cr.Compiled.Counters = append(cr.Compiled.Counters, CompiledCounter{Name: c.Name, Pattern: rx})
	}
	for _, om := range r.MatchOutput {
		rx, err := compilePatternWithFlags(om.Pattern, om.Flags)
		if err != nil {
			slog.Warn("tokenjuice: output match compile failed", "id", r.ID, "err", err)
			continue
		}
		cr.Compiled.OutputMatches = append(cr.Compiled.OutputMatches,
			CompiledOutputMatch{Pattern: rx, Message: om.Message})
	}
	return cr, nil
}

// compilePatternWithFlags wraps a regex pattern with (?flags) when flags are set.
func compilePatternWithFlags(pattern, flags string) (*regexp.Regexp, error) {
	if flags == "" {
		return regexp.Compile(pattern)
	}
	return regexp.Compile("(?" + flags + ")" + pattern)
}

// syntheticFallback returns a minimal in-memory generic/fallback rule.
// Used when the embedded rule FS fails to load (boot non-fatal).
func syntheticFallback() []*CompiledRule {
	head, tail := 8, 8
	return []*CompiledRule{{
		Rule: Rule{
			ID:        "generic/fallback",
			Family:    "generic",
			Summarize: &RuleSummarize{Head: &head, Tail: &tail},
			Transforms: &RuleTransforms{
				StripAnsi:      true,
				TrimEmptyEdges: true,
				DedupeAdjacent: true,
			},
		},
		Source: RuleOriginBuiltin,
		Path:   "builtin:synthetic",
	}}
}
