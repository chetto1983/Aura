// Package packs reads a knowledge-work plugin as ONE unit.
//
// The skills catalogue Aura already speaks (skills.sh) flattens a plugin away:
// it answers `{id, skillId, name, installs, source}` and nothing else, so
// `call-prep` arrives as `anthropics/knowledge-work-plugins/call-prep` with the
// `sales/` folder it belongs to dropped, and with no sight of the connectors or
// commands that ship beside it. Measured 2026-08-23 (amendment #126): eighteen
// job-function plugins reach the catalogue as at least a hundred loose skills.
//
// This package recovers the grouping from the one thing the catalogue does
// carry — the `source` repository — by reading the four files Anthropic's open
// plugin format defines:
//
//	<plugin>/.claude-plugin/plugin.json   the manifest
//	<plugin>/skills/<name>/SKILL.md       the skills
//	<plugin>/.mcp.json                    the connectors
//	<plugin>/commands/<name>.md           the commands
//
// It resolves and describes. It installs nothing: skills go through the
// installer that already exists, and connectors through the MCP install path
// that already exists, so that neither acquires a second implementation.
package packs

import (
	"fmt"
	"strings"
)

// Ref names what to resolve. An empty Directory means EVERY pack in the
// repository, which is the normal case for a monorepo marketplace — the caller
// then chooses. A named Directory means that one pack, and its absence is an
// error rather than an empty result, because "I asked for sales and got
// nothing" must not read the same as "this repo has no packs".
type Ref struct {
	Source    string
	Directory string
}

// ParseRef reads `owner/repo` or `owner/repo/plugin`.
//
// It is deliberately NOT the skills installer's `owner/repo@skill` syntax: that
// one addresses a single skill and this one addresses a bundle, and letting the
// two blur would make `sales@call-prep` look meaningful when it is not.
func ParseRef(s string) (Ref, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(s), "/"), "/")
	for _, p := range parts {
		if err := validSegment(p); err != nil {
			return Ref{}, err
		}
	}
	switch len(parts) {
	case 2:
		return Ref{Source: parts[0] + "/" + parts[1]}, nil
	case 3:
		return Ref{Source: parts[0] + "/" + parts[1], Directory: parts[2]}, nil
	default:
		return Ref{}, fmt.Errorf("pack ref %q: want owner/repo or owner/repo/plugin", s)
	}
}

// validSegment rejects anything that could leave the repository once the
// Directory is joined onto a checkout path. The resolver reads from a directory
// a fetcher just wrote, so a `..` here is a path traversal against the host, not
// merely a lookup that misses.
func validSegment(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("pack ref: empty path segment")
	case p == "." || p == "..":
		return fmt.Errorf("pack ref: %q is not a name", p)
	case strings.ContainsAny(p, `\:*?"<>|`):
		return fmt.Errorf("pack ref: %q contains a path character", p)
	}
	return nil
}

// String renders a Ref the way an operator wrote it.
func (r Ref) String() string {
	if r.Directory == "" {
		return r.Source
	}
	return r.Source + "/" + r.Directory
}

// Pack is one plugin, whole.
type Pack struct {
	Source      string
	Directory   string
	Name        string
	Version     string
	Description string
	Author      string

	// Skills carries NAMES, not bodies. The installer materializes a skill from
	// `source@name` and Aura's own loader parses its frontmatter; re-parsing it
	// here would produce a second, drifting reading of the same file.
	Skills   []string
	Servers  []Server
	Commands []Command
}

// Ref reconstructs the reference that resolves to exactly this pack.
func (p Pack) Ref() Ref { return Ref{Source: p.Source, Directory: p.Directory} }

// SkillRefs renders each skill in the syntax the existing installer accepts, so
// installing a pack's skills is a loop over this and nothing more.
func (p Pack) SkillRefs() []string {
	refs := make([]string, 0, len(p.Skills))
	for _, s := range p.Skills {
		refs = append(refs, p.Source+"@"+s)
	}
	return refs
}

// Server is one entry of a pack's `.mcp.json`, in the shape that file uses.
// It is NOT mcp.ManagedServer: this package describes what a third party wrote,
// and the translation into Aura's own trust-bearing type belongs at the install
// boundary, where the operator's approval is recorded.
type Server struct {
	Name    string
	Type    string
	URL     string
	Command string
	Args    []string
	Env     map[string]string

	// OAuth reports whether the entry declared an `oauth` block. A connector
	// that needs a browser round-trip cannot be brought up unattended, and the
	// operator has to be told which ones those are BEFORE they approve a pack.
	OAuth bool
}

// Command is one `commands/<name>.md`.
type Command struct {
	Name         string
	Description  string
	ArgumentHint string
	Body         string
}
