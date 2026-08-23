package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/packs"
)

// salesPack mirrors what the resolver returns for the `sales` plugin of
// anthropics/knowledge-work-plugins as measured 2026-08-23: an OAuth connector
// beside plain ones, and one command against several skills.
func salesPack() packs.Pack {
	return packs.Pack{
		Source:      "anthropics/knowledge-work-plugins",
		Directory:   "sales",
		Name:        "sales",
		Version:     "1.3.1",
		Description: "Research prospects, prep for calls, review your pipeline.",
		Author:      "Anthropic",
		Skills:      []string{"call-prep", "forecast"},
		Servers: []packs.Server{
			{Name: "hubspot", Type: "http", URL: "https://mcp.hubspot.com/anthropic"},
			{Name: "notes", Type: "stdio", Command: "notes-mcp", Args: []string{"--vault", "/data"}},
			{Name: "slack", Type: "http", URL: "https://mcp.slack.com/mcp", OAuth: true},
		},
		Commands: []packs.Command{
			{Name: "call-prep", Description: "Prep me for a call", ArgumentHint: "[company]"},
		},
	}
}

func withResolver(t *testing.T, fn func(context.Context, packs.Ref) ([]packs.Pack, error)) {
	t.Helper()
	prev := packResolve
	packResolve = fn
	t.Cleanup(func() { packResolve = prev })
}

func TestPackShowRendersEveryPlane(t *testing.T) {
	withResolver(t, func(_ context.Context, ref packs.Ref) ([]packs.Pack, error) {
		if ref.Directory != "sales" {
			t.Errorf("resolved %+v", ref)
		}
		return []packs.Pack{salesPack()}, nil
	})

	var out bytes.Buffer
	if err := packCommand(t.Context(), "show", "anthropics/knowledge-work-plugins/sales", false, &out); err != nil {
		t.Fatalf("packCommand: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"sales v1.3.1",
		"author: Anthropic",
		"anthropics/knowledge-work-plugins/sales",
		"skills (2)",
		"anthropics/knowledge-work-plugins@call-prep",
		"connectors (3)",
		"commands (1)",
		"/call-prep",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
}

// The oauth marker is the whole reason the connector list is rendered before an
// operator approves a pack: it is the one entry that cannot be brought up
// unattended, and it must be visible without reading the JSON.
func TestPackShowMarksTheConnectorThatNeedsOAuth(t *testing.T) {
	withResolver(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
		return []packs.Pack{salesPack()}, nil
	})

	var out bytes.Buffer
	if err := packCommand(t.Context(), "show", "o/r/sales", false, &out); err != nil {
		t.Fatalf("packCommand: %v", err)
	}
	for line := range strings.SplitSeq(out.String(), "\n") {
		marked := strings.Contains(line, "[needs oauth]")
		if strings.Contains(line, "slack") != marked && strings.Contains(line, "mcp.") {
			t.Errorf("oauth marker is on the wrong connector: %q", line)
		}
	}
}

// A stdio connector has no URL; showing an empty column would hide what it runs.
func TestPackShowRendersAStdioConnectorAsItsCommand(t *testing.T) {
	withResolver(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
		return []packs.Pack{salesPack()}, nil
	})

	var out bytes.Buffer
	_ = packCommand(t.Context(), "show", "o/r/sales", false, &out)
	if !strings.Contains(out.String(), "notes-mcp --vault /data") {
		t.Errorf("stdio connector rendered without its argv:\n%s", out.String())
	}
}

func TestPackListSummarizesEveryPackInARepository(t *testing.T) {
	withResolver(t, func(_ context.Context, ref packs.Ref) ([]packs.Pack, error) {
		if ref.Directory != "" {
			t.Errorf("list resolved a single directory: %+v", ref)
		}
		legal := packs.Pack{Source: ref.Source, Directory: "legal", Name: "legal", Skills: []string{"nda"}}
		return []packs.Pack{legal, salesPack()}, nil
	})

	var out bytes.Buffer
	if err := packCommand(t.Context(), "list", "anthropics/knowledge-work-plugins", false, &out); err != nil {
		t.Fatalf("packCommand: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "2 pack(s)") {
		t.Errorf("no count in:\n%s", got)
	}
	for _, want := range []string{"legal", "sales", "2 skills", "3 connectors"} {
		if !strings.Contains(got, want) {
			t.Errorf("list is missing %q:\n%s", want, got)
		}
	}
}

// A pack with no version renders a dash rather than a bare "v", which would read
// as a version that exists and is empty.
func TestPackListRendersAMissingVersionAsADash(t *testing.T) {
	withResolver(t, func(_ context.Context, ref packs.Ref) ([]packs.Pack, error) {
		return []packs.Pack{{Source: ref.Source, Name: "bare"}}, nil
	})

	var out bytes.Buffer
	_ = packCommand(t.Context(), "list", "o/r", false, &out)
	if !strings.Contains(out.String(), "bare") || strings.Contains(out.String(), " v ") {
		t.Errorf("version column: %q", out.String())
	}
}

func TestPackJSONIsTheWholeResolvedPack(t *testing.T) {
	withResolver(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
		return []packs.Pack{salesPack()}, nil
	})

	var out bytes.Buffer
	if err := packCommand(t.Context(), "show", "o/r/sales", true, &out); err != nil {
		t.Fatalf("packCommand: %v", err)
	}
	var got []packs.Pack
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json did not emit JSON: %v\n%s", err, out.String())
	}
	if len(got) != 1 || got[0].Name != "sales" || len(got[0].Servers) != 3 {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if !got[0].Servers[2].OAuth {
		t.Error("the oauth flag did not survive JSON")
	}
}

// `show` on a repository is ambiguous when it holds several plugins. Showing the
// first would be a guess presented as an answer.
func TestPackShowRefusesARepositoryWithoutAPlugin(t *testing.T) {
	withResolver(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
		t.Fatal("show resolved before rejecting an ambiguous reference")
		return nil, nil
	})

	err := packCommand(t.Context(), "show", "anthropics/knowledge-work-plugins", false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "aura pack list") {
		t.Fatalf("err = %v, want a refusal pointing at list", err)
	}
}

func TestPackRejectsAMalformedReferenceBeforeResolving(t *testing.T) {
	withResolver(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
		t.Fatal("a malformed reference reached the resolver")
		return nil, nil
	})

	if err := packCommand(t.Context(), "list", "https://evil.example/x", false, &bytes.Buffer{}); err == nil {
		t.Fatal("a reference carrying its own host was accepted")
	}
}

func TestPackSurfacesAResolveFailure(t *testing.T) {
	boom := errors.New("git clone failed: repository not found")
	withResolver(t, func(context.Context, packs.Ref) ([]packs.Pack, error) { return nil, boom })

	err := packCommand(t.Context(), "list", "owner/nope", false, &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the resolver's own failure", err)
	}
}

// Two of sales' fourteen entries ship an empty url. A blank column let a
// placeholder read as a connector; it has to say it points nowhere.
func TestPackShowNamesAConnectorThatPointsNowhere(t *testing.T) {
	withResolver(t, func(_ context.Context, ref packs.Ref) ([]packs.Pack, error) {
		return []packs.Pack{{Source: ref.Source, Name: "sales", Servers: []packs.Server{
			{Name: "gmail", Type: "http"},
			{Name: "hubspot", Type: "http", URL: "https://mcp.hubspot.com/anthropic"},
		}}}, nil
	})

	var out bytes.Buffer
	if err := packCommand(t.Context(), "show", "o/r/sales", false, &out); err != nil {
		t.Fatalf("packCommand: %v", err)
	}
	if !strings.Contains(out.String(), "placeholder") {
		t.Errorf("an empty url rendered silently:\n%s", out.String())
	}
}
