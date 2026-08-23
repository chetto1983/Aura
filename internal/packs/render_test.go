package packs

import (
	"bytes"
	"strings"
	"testing"
)

// The renderer lives here because TWO surfaces show a pack — `aura pack` for the
// operator and the `plugin_pack` tool for the model — and the tests live here
// with it for the same reason: a divergence between what the two see is the bug
// this arrangement exists to make impossible.

func renderFixture() Pack {
	return Pack{
		Source: "anthropics/knowledge-work-plugins", Directory: "sales",
		Name: "sales", Version: "1.3.0", Author: "Anthropic",
		Description: "Research prospects and prep for calls.",
		Skills:      []string{"call-prep", "forecast"},
		Servers: []Server{
			{Name: "gmail", Type: "http"},
			{Name: "hubspot", Type: "http", URL: "https://mcp.hubspot.com/anthropic"},
			{Name: "notes", Type: "stdio", Command: "notes-mcp", Args: []string{"--vault", "/data"}},
			{Name: "slack", Type: "http", URL: "https://mcp.slack.com/mcp", OAuth: true},
		},
		Commands: []Command{{Name: "call-prep", Description: "Prep me for a call", ArgumentHint: "[company]"}},
	}
}

func TestWriteDetailRendersEveryPlane(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteDetail(&out, renderFixture())
	got := out.String()

	for _, want := range []string{
		"sales v1.3.0",
		"author: Anthropic",
		"source: anthropics/knowledge-work-plugins/sales",
		"Research prospects",
		"skills (2)",
		// The installer syntax, so a line can be handed straight to skill_manage.
		"anthropics/knowledge-work-plugins@call-prep",
		"connectors (4)",
		"https://mcp.hubspot.com/anthropic",
		"notes-mcp --vault /data",
		"commands (1)",
		"/call-prep",
		"[company]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail is missing %q:\n%s", want, got)
		}
	}
}

// The oauth marker is the one connector fact an operator needs BEFORE approving
// a pack, and it must land on that connector's line and no other.
func TestWriteDetailMarksOnlyTheOAuthConnector(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteDetail(&out, renderFixture())

	marked := 0
	for line := range strings.SplitSeq(out.String(), "\n") {
		if !strings.Contains(line, "[needs oauth]") {
			continue
		}
		marked++
		if !strings.Contains(line, "slack") {
			t.Errorf("the oauth marker landed on the wrong connector: %q", line)
		}
	}
	if marked != 1 {
		t.Errorf("oauth marker appears %d times, want 1", marked)
	}
}

func TestWriteDetailOmitsAnAuthorAndDescriptionItDoesNotHave(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteDetail(&out, Pack{Source: "me/solo", Name: "solo"})
	got := out.String()

	if strings.Contains(got, "author:") {
		t.Errorf("an absent author was rendered:\n%s", got)
	}
	if !strings.Contains(got, "solo -") {
		t.Errorf("a missing version should render as a dash:\n%s", got)
	}
	for _, want := range []string{"skills (0)", "connectors (0)", "commands (0)"} {
		if !strings.Contains(got, want) {
			t.Errorf("an empty plane must still be counted, missing %q:\n%s", want, got)
		}
	}
}

func TestWriteListSummarizesOneLinePerPack(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteList(&out, []Pack{renderFixture(), {Source: "o/r", Directory: "legal", Name: "legal"}})
	got := out.String()

	if !strings.Contains(got, "2 pack(s)") {
		t.Errorf("no count:\n%s", got)
	}
	for _, want := range []string{
		"sales", "v1.3.0", "2 skills", "4 connectors", "1 commands",
		"anthropics/knowledge-work-plugins/sales",
		"legal", "o/r/legal",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("list is missing %q:\n%s", want, got)
		}
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines != 2 {
		t.Errorf("want a header and one line per pack, got %d newlines:\n%s", lines, got)
	}
}

func TestConnectorTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Server
		want string
	}{
		{"http", Server{URL: "https://mcp.slack.com/mcp"}, "https://mcp.slack.com/mcp"},
		{"stdio with args", Server{Command: "notes-mcp", Args: []string{"-v", "/d"}}, "notes-mcp -v /d"},
		{"stdio bare", Server{Command: "notes-mcp"}, "notes-mcp"},
		// The measured case: two of sales' fourteen ship an empty url, and a
		// blank column let a placeholder read as a connector.
		{"placeholder", Server{Type: "http"}, "(no endpoint declared — placeholder)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ConnectorTarget(tt.in); got != tt.want {
				t.Errorf("ConnectorTarget = %q, want %q", got, tt.want)
			}
		})
	}
}

// A pack whose connector declares no transport still has to render a column; a
// bare "" would shift every field after it.
func TestWriteDetailRendersADashForAMissingType(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteDetail(&out, Pack{Name: "p", Servers: []Server{{Name: "x", URL: "https://e.example"}}})
	if !strings.Contains(out.String(), "x              -        https://e.example") {
		t.Errorf("missing type did not render as a dash:\n%s", out.String())
	}
}
