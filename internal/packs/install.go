package packs

import (
	"context"
	"fmt"
	"strings"
)

// SourceMarker is what a pack writes into the Source field of every connector it
// installs. It is how a pack's membership survives without a table of its own:
// the field is already persisted beside each managed server, so "which connectors
// came from sales" is a filter, not a join, and the single trust-approve the
// operator grants per pack has something to range over.
//
// The prefix is deliberate and reserved. Aura's own sources are bare words
// ("manual", "custom", "env:AURA_MCP_SERVERS_JSON"), so `pack:` cannot collide
// with one, and a human reading servers.json can see where a server came from
// without consulting anything else.
func SourceMarker(ref Ref) string { return "pack:" + ref.String() }

// RefFromSource reverses SourceMarker, reporting false for a source that no pack
// wrote. A caller iterating a whole managed config uses this to pick out the
// members of one pack.
func RefFromSource(source string) (Ref, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(source), "pack:")
	if !ok {
		return Ref{}, false
	}
	ref, err := ParseRef(rest)
	if err != nil {
		return Ref{}, false
	}
	return ref, true
}

// StepResult is one skill or one connector, and what happened to it.
type StepResult struct {
	Name string
	Err  error
}

// Report is what an install did, in full.
//
// It is a report and not an error because a pack is many things at once: sixteen
// of Anthropic's plugins ship between four and thirty-one skills, and aborting
// the whole install because the twelfth skill was withdrawn from the catalogue
// would leave the operator with a half-installed pack and no account of it. Every
// step runs; every outcome is recorded; the caller decides what to do about the
// failures.
type Report struct {
	Pack    Pack
	Skills  []StepResult
	Servers []StepResult
}

// Failed reports how many steps did not land.
func (r Report) Failed() int {
	n := 0
	for _, s := range append(append([]StepResult{}, r.Skills...), r.Servers...) {
		if s.Err != nil {
			n++
		}
	}
	return n
}

// Installer wires a pack's two halves to the machinery that already installs
// each of them. Neither is implemented here: a skill goes through the same
// Installer the cockpit and the model use, and a connector through the same
// managed-config write the `aura mcp add` path uses, so a pack install cannot
// drift from a single install and cannot bypass the audit either records.
type Installer struct {
	// InstallSkill receives the `owner/repo@skill` form the existing installer
	// accepts. Nil skips the skills half, which is what a caller wanting only
	// the connectors passes.
	InstallSkill func(ctx context.Context, sourceRef string) error

	// AddServer receives the connector under the name the pack gave it, already
	// marked with SourceMarker. Nil skips the connectors half.
	AddServer func(ctx context.Context, name string, server Server) error
}

// Install runs every step and returns the account of it. The context is checked
// between steps so a cancelled install stops promptly rather than working
// through thirty more skills that nobody is waiting for.
func (i Installer) Install(ctx context.Context, pack Pack) (Report, error) {
	report := Report{Pack: pack}

	if i.InstallSkill != nil {
		for _, ref := range pack.SkillRefs() {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			report.Skills = append(report.Skills, StepResult{Name: ref, Err: i.InstallSkill(ctx, ref)})
		}
	}

	if i.AddServer != nil {
		for _, s := range pack.Servers {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			report.Servers = append(report.Servers, StepResult{Name: s.Name, Err: i.AddServer(ctx, s.Name, s)})
		}
	}
	return report, nil
}

// InstallableServers splits a pack's connectors into the ones worth installing
// and the ones that would install a name pointing at nothing.
//
// Two of sales' fourteen ship `"url": ""` — they are placeholders the format
// allows, and adding them would put a server in the managed config that can
// never connect, that the operator would then have to approve, and that the
// model would see in its manifest as a capability it does not have.
func InstallableServers(pack Pack) (install []Server, skipped []Server) {
	for _, s := range pack.Servers {
		if s.URL == "" && strings.TrimSpace(s.Command) == "" {
			skipped = append(skipped, s)
			continue
		}
		install = append(install, s)
	}
	return install, skipped
}

// WriteReport renders a Report for a human.
func WriteReport(out interface{ Write([]byte) (int, error) }, r Report) {
	_, _ = fmt.Fprintf(out, "%s %s — %d skill(s), %d connector(s)\n",
		r.Pack.Name, versionOrDash(r.Pack.Version), len(r.Skills), len(r.Servers))
	writeSteps(out, "skills", r.Skills)
	writeSteps(out, "connectors", r.Servers)
	if r.Failed() > 0 {
		_, _ = fmt.Fprintf(out, "\n%d step(s) failed; the rest landed.\n", r.Failed())
	}
}

func writeSteps(out interface{ Write([]byte) (int, error) }, label string, steps []StepResult) {
	if len(steps) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\n%s\n", label)
	for _, s := range steps {
		if s.Err != nil {
			_, _ = fmt.Fprintf(out, "  FAILED  %s — %v\n", s.Name, s.Err)
			continue
		}
		_, _ = fmt.Fprintf(out, "  ok      %s\n", s.Name)
	}
}
