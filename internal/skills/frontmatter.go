// Package skills is the read half of Aura's skills system (Slice 7a): a multi-root
// loader that scans SKILL.md files on disk, parses their YAML frontmatter, and
// caches the result behind a short TTL. The disk is operator-trusted (D-28): the
// loader validates STRUCTURE only (parse + name regex + body cap), never a
// content blocklist — that enforcement lives at the write boundaries (plan 11-04).
package skills

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"golang.org/x/text/unicode/norm"
)

// Frontmatter is the parsed YAML header of a SKILL.md (D-30). Only Name and
// Description are required; every other field is tolerated-optional so a real
// third-party skill (license, compatibility, allowed-tools, arbitrary metadata)
// parses without error. The snippet-docs fields (Language..NeedsWorkspace) are
// inert documentation here — the snippet runtime (plan 11-05) consumes them.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Always      bool   `yaml:"always"`
	Type        string `yaml:"type"`

	// Tolerated-inert instruction-skill fields. Present so goccy does not have to
	// be lax about THESE keys — they are captured, just unused on the read path.
	License       string         `yaml:"license"`
	Compatibility string         `yaml:"compatibility"`
	Metadata      map[string]any `yaml:"metadata"`
	AllowedTools  []string       `yaml:"allowed-tools"`

	// Snippet-docs fields (D-30). Documentation-only on the read path; the snippet
	// runtime (plan 11-05) is their consumer. `deps:` is never a runtime install.
	Language       string         `yaml:"language"`
	InputsSchema   map[string]any `yaml:"inputs_schema"`
	OutputsDesc    string         `yaml:"outputs_desc"`
	Deps           []string       `yaml:"deps"`
	Tags           []string       `yaml:"tags"`
	NeedsNetwork   bool           `yaml:"needs_network"`
	NeedsWorkspace bool           `yaml:"needs_workspace"`
}

// TypeInstruction / TypeSnippet are the two skill kinds. Type defaults to
// instruction when the frontmatter omits it (D-30).
const (
	TypeInstruction = "instruction"
	TypeSnippet     = "snippet"
)

// parseFrontmatter CRLF-normalizes the raw SKILL.md, splits the leading
// `---`...`---` YAML block, parses it with goccy/go-yaml, and returns the parsed
// Frontmatter plus the markdown body that follows the closing fence. goccy is the
// load-bearing choice (D-30): the real skills.sh corpus (spike 006) ships a
// double-quoted `description` scalar with escaped inner quotes, which a hand-rolled
// `key: value` splitter (picobot's anti-reference) mis-parses. Type defaults to
// instruction; unknown keys are tolerated (goccy ignores struct-absent fields).
func parseFrontmatter(raw []byte) (Frontmatter, string, error) {
	block, body, err := SplitFrontmatter(raw)
	if err != nil {
		return Frontmatter{}, "", err
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return Frontmatter{}, "", fmt.Errorf("parse frontmatter yaml: %w", err)
	}
	if fm.Type == "" {
		fm.Type = TypeInstruction
	}
	// NFKC-normalize the identity fields (TR15 compatibility composition) so a name
	// encoded with fullwidth/compatibility codepoints folds to its canonical form
	// before the loader's name-regex + dir-name match — the same canonical form the
	// write-boundary validator (plan 11-04) blocklists against (D-27).
	fm.Name = norm.NFKC.String(fm.Name)
	fm.Description = norm.NFKC.String(fm.Description)
	return fm, body, nil
}

// SplitFrontmatter extracts the YAML between the leading `---` and the next `---`
// on its own line, returning the YAML bytes and the remaining markdown body. A
// SKILL.md MUST open with a frontmatter fence — a missing one is an error (the
// loader skip-logs it; the file is not a valid skill).
//
// Exported because a SKILL.md is not the only file in Aura that opens with this
// fence: a knowledge-work pack's `commands/*.md` uses the identical convention
// (amendment #126), and `internal/packs` reads those. One fence rule, in one
// place — including the CRLF normalization, which is not optional and was the
// half a second implementation would most likely have dropped.
func SplitFrontmatter(raw []byte) (block []byte, body string, err error) {
	// Normalize CRLF → LF first: Windows checkouts (spike 004b) and the skills.sh
	// corpus arrive CRLF, and the fence split below is newline-sensitive.
	s := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	// Tolerate a leading BOM or blank lines before the fence is NOT done here —
	// SKILL.md opens with the fence (CC parity). Trim only a leading newline run.
	trimmed := strings.TrimLeft(s, "\n")
	if !strings.HasPrefix(trimmed, "---\n") && trimmed != "---" {
		return nil, "", fmt.Errorf("frontmatter: missing leading '---' fence")
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	// Find the closing fence: a line that is exactly "---".
	idx := indexClosingFence(rest)
	if idx < 0 {
		return nil, "", fmt.Errorf("frontmatter: missing closing '---' fence")
	}
	block = []byte(rest[:idx])
	body = strings.TrimPrefix(rest[idx:], "---")
	body = strings.TrimPrefix(body, "\n")
	return block, body, nil
}

// indexClosingFence returns the byte offset of the closing "---" line within rest
// (the bytes after the opening fence), or -1 if none. The fence must be a full
// line: either "---\n" or a trailing "---" at EOF.
func indexClosingFence(rest string) int {
	off := 0
	for {
		nl := strings.IndexByte(rest[off:], '\n')
		var line string
		if nl < 0 {
			line = rest[off:]
		} else {
			line = rest[off : off+nl]
		}
		if line == "---" {
			return off
		}
		if nl < 0 {
			return -1
		}
		off += nl + 1
	}
}
