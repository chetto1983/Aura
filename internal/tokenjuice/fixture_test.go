package tokenjuice_test

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/aura/aura/internal/tokenjuice"
)

//go:embed tests/fixtures/*.json
var fixtureFS embed.FS

// fixtureInput mirrors the JSON "input" object in each fixture file.
type fixtureInput struct {
	ToolName string   `json:"toolName"`
	Argv     []string `json:"argv"`
	Command  string   `json:"command,omitempty"`
	Stdout   string   `json:"stdout"`
	ExitCode *int     `json:"exitCode,omitempty"`
}

// fixture is the full fixture file shape.
type fixture struct {
	Description          string       `json:"description"`
	Input                fixtureInput `json:"input"`
	ExpectApplied        *bool        `json:"expectApplied,omitempty"`
	ExpectedOutput       string       `json:"expectedOutput,omitempty"`
	ExpectedContains     []string     `json:"expectedContains,omitempty"`
	ExpectedNotContains  []string     `json:"expectedNotContains,omitempty"`
}

func TestFixtures(t *testing.T) {
	entries, err := fs.ReadDir(fixtureFS, "tests/fixtures")
	if err != nil {
		t.Fatalf("readdir tests/fixtures: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(fixtureFS, "tests/fixtures/"+name)
			if err != nil {
				t.Fatalf("read fixture %s: %v", name, err)
			}
			var fix fixture
			if err := json.Unmarshal(data, &fix); err != nil {
				t.Fatalf("parse fixture %s: %v", name, err)
			}

			in := tokenjuice.Input{
				ToolName: fix.Input.ToolName,
				Argv:     fix.Input.Argv,
				Command:  fix.Input.Command,
				Stdout:   fix.Input.Stdout,
				ExitCode: fix.Input.ExitCode,
			}
			r := tokenjuice.Compact(in, tokenjuice.Options{})

			// Assert Applied flag if specified.
			if fix.ExpectApplied != nil && r.Applied != *fix.ExpectApplied {
				t.Errorf("[%s] Applied: want %v, got %v (RuleID=%q, bytes_in=%d, bytes_out=%d)",
					fix.Description, *fix.ExpectApplied, r.Applied, r.RuleID,
					r.Stats.OriginalBytes, r.Stats.CompactedBytes)
			}

			// Assert exact output if specified.
			if fix.ExpectedOutput != "" && r.InlineText != fix.ExpectedOutput {
				t.Errorf("[%s] InlineText mismatch:\nwant: %q\ngot:  %q",
					fix.Description, fix.ExpectedOutput, r.InlineText)
			}

			// Assert required substrings.
			for _, want := range fix.ExpectedContains {
				if !strings.Contains(r.InlineText, want) {
					t.Errorf("[%s] InlineText missing %q:\n%s",
						fix.Description, want, r.InlineText)
				}
			}

			// Assert forbidden substrings.
			for _, forbid := range fix.ExpectedNotContains {
				if strings.Contains(r.InlineText, forbid) {
					t.Errorf("[%s] InlineText should not contain %q:\n%s",
						fix.Description, forbid, r.InlineText)
				}
			}
		})
	}
}
