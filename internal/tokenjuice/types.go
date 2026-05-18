package tokenjuice

import "regexp"

// Input describes one tool invocation for compaction.
type Input struct {
	ToolName string
	Argv     []string
	Command  string
	Stdout   string
	Stderr   string
	ExitCode *int
}

// Options controls compaction behaviour. Zero values use upstream defaults.
type Options struct {
	MaxInlineChars int
	ForceRuleID    string
	Raw            bool
	MinInputBytes  int
	MinRatio       float64
}

// Stats holds per-call compaction metrics.
type Stats struct {
	ToolName       string
	OriginalBytes  int
	CompactedBytes int
	OriginalChars  int
	CompactedChars int
	Family         string
	Confidence     float64
}

// Ratio returns CompactedBytes / OriginalBytes, or 1.0 when OriginalBytes == 0.
func (s Stats) Ratio() float64 {
	if s.OriginalBytes == 0 {
		return 1.0
	}
	return float64(s.CompactedBytes) / float64(s.OriginalBytes)
}

// Result is what the caller inlines into the LLM tool message.
type Result struct {
	InlineText string
	Applied    bool
	RuleID     string
	Stats      Stats
}

// ClassificationResult holds the output of rule classification.
type ClassificationResult struct {
	Family         string
	Confidence     float64
	MatchedReducer *string
}

// RuleMatch holds classification predicate fields.
type RuleMatch struct {
	ToolNames          []string   `json:"toolNames,omitempty"`
	Argv0              []string   `json:"argv0,omitempty"`
	ArgvIncludes       [][]string `json:"argvIncludes,omitempty"`
	ArgvIncludesAny    [][]string `json:"argvIncludesAny,omitempty"`
	CommandIncludes    []string   `json:"commandIncludes,omitempty"`
	CommandIncludesAny []string   `json:"commandIncludesAny,omitempty"`
}

// RulePattern is a regex pattern with optional flags.
type RulePattern struct {
	Pattern string `json:"pattern"`
	Flags   string `json:"flags,omitempty"`
}

// RuleFilters holds skip/keep pattern lists.
type RuleFilters struct {
	SkipPatterns []RulePattern `json:"skipPatterns,omitempty"`
	KeepPatterns []RulePattern `json:"keepPatterns,omitempty"`
}

// RuleTransforms holds boolean transform flags.
type RuleTransforms struct {
	StripAnsi       bool `json:"stripAnsi,omitempty"`
	TrimEmptyEdges  bool `json:"trimEmptyEdges,omitempty"`
	DedupeAdjacent  bool `json:"dedupeAdjacent,omitempty"`
	PrettyPrintJSON bool `json:"prettyPrintJson,omitempty"`
}

// RuleSummarize configures head/tail window sizes.
type RuleSummarize struct {
	Head *int `json:"head,omitempty"`
	Tail *int `json:"tail,omitempty"`
}

// RuleCounter is a named regex pattern that counts occurrences.
type RuleCounter struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Flags   string `json:"flags,omitempty"`
}

// RuleOutputMatch maps a regex pattern to a canned replacement message.
type RuleOutputMatch struct {
	Pattern string `json:"pattern"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// RuleFailure holds failure-mode head/tail overrides.
type RuleFailure struct {
	PreserveOnFailure bool `json:"preserveOnFailure,omitempty"`
	Head              *int `json:"head,omitempty"`
	Tail              *int `json:"tail,omitempty"`
}

// Rule is the JSON-deserialized rule definition (corresponds to JsonRule in the spec).
type Rule struct {
	ID            string            `json:"id"`
	Family        string            `json:"family"`
	Description   *string           `json:"description,omitempty"`
	Priority      *int32            `json:"priority,omitempty"`
	OnEmpty       *string           `json:"onEmpty,omitempty"`
	MatchOutput   []RuleOutputMatch `json:"matchOutput,omitempty"`
	CounterSource *string           `json:"counterSource,omitempty"`
	Match         RuleMatch         `json:"match"`
	Filters       *RuleFilters      `json:"filters,omitempty"`
	Transforms    *RuleTransforms   `json:"transforms,omitempty"`
	Summarize     *RuleSummarize    `json:"summarize,omitempty"`
	Counters      []RuleCounter     `json:"counters,omitempty"`
	Failure       *RuleFailure      `json:"failure,omitempty"`
}

// RuleOrigin indicates where a rule was loaded from.
type RuleOrigin int

const (
	RuleOriginBuiltin RuleOrigin = iota
	RuleOriginUser
	RuleOriginProject
)

// CompiledCounter is a named compiled regex counter.
type CompiledCounter struct {
	Name    string
	Pattern *regexp.Regexp
}

// CompiledOutputMatch is a compiled output-match pattern.
type CompiledOutputMatch struct {
	Pattern *regexp.Regexp
	Message string
}

// CompiledParts holds pre-compiled regex slices for a rule.
type CompiledParts struct {
	SkipPatterns  []*regexp.Regexp
	KeepPatterns  []*regexp.Regexp
	Counters      []CompiledCounter
	OutputMatches []CompiledOutputMatch
}

// CompiledRule is a Rule with all regex patterns pre-compiled at load time.
type CompiledRule struct {
	Rule     Rule
	Source   RuleOrigin
	Path     string
	Compiled CompiledParts
}
