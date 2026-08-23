package packs

import (
	"fmt"
	"io"
	"strings"
)

// The rendering lives here, beside the type it renders, because TWO surfaces
// show a pack: `aura pack` for the operator and the `plugin_pack` tool for the model.
// Two copies would drift, and the model's copy drifting is the one nobody sees
// until it describes a connector that is not there.

// WriteList summarizes every pack in a repository, one line each.
func WriteList(out io.Writer, found []Pack) {
	_, _ = fmt.Fprintf(out, "%d pack(s)\n", len(found))
	for _, p := range found {
		_, _ = fmt.Fprintf(out, "  %-28s %-10s %2d skills  %2d connectors  %2d commands  %s\n",
			p.Name, versionOrDash(p.Version), len(p.Skills), len(p.Servers), len(p.Commands),
			p.Ref().String())
	}
}

// WriteDetail renders one pack in full.
func WriteDetail(out io.Writer, p Pack) {
	_, _ = fmt.Fprintf(out, "%s %s\n", p.Name, versionOrDash(p.Version))
	if p.Author != "" {
		_, _ = fmt.Fprintf(out, "author: %s\n", p.Author)
	}
	_, _ = fmt.Fprintf(out, "source: %s\n", p.Ref().String())
	if p.Description != "" {
		_, _ = fmt.Fprintf(out, "\n%s\n", p.Description)
	}

	// The installer syntax rather than the bare name, so a line can be handed
	// straight to skill_manage without the reader deriving the form.
	_, _ = fmt.Fprintf(out, "\nskills (%d)\n", len(p.Skills))
	for _, ref := range p.SkillRefs() {
		_, _ = fmt.Fprintf(out, "  %s\n", ref)
	}

	_, _ = fmt.Fprintf(out, "\nconnectors (%d)\n", len(p.Servers))
	for _, s := range p.Servers {
		// The oauth marker is the one fact needed before approving a pack: a
		// browser round-trip cannot be completed unattended.
		note := ""
		if s.OAuth {
			note = "  [needs oauth]"
		}
		_, _ = fmt.Fprintf(out, "  %-14s %-8s %s%s\n", s.Name, orDash(s.Type), ConnectorTarget(s), note)
	}

	_, _ = fmt.Fprintf(out, "\ncommands (%d)\n", len(p.Commands))
	for _, c := range p.Commands {
		_, _ = fmt.Fprintf(out, "  /%-16s %s %s\n", c.Name, c.ArgumentHint, c.Description)
	}
}

// ConnectorTarget names where a connector points, and says so out loud when it
// points nowhere. Two of sales' fourteen entries ship `"url": ""` (gmail and
// google calendar, measured 2026-08-23) — they are placeholders, not endpoints,
// and a blank column let a non-connector read as a connector.
func ConnectorTarget(s Server) string {
	if s.URL != "" {
		return s.URL
	}
	if argv := strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " ")); argv != "" {
		return argv
	}
	return "(no endpoint declared — placeholder)"
}

func versionOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return "v" + v
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
