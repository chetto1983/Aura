package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/packs"
)

func packFixture() []packs.Pack {
	return []packs.Pack{{
		Source: "anthropics/knowledge-work-plugins", Directory: "sales",
		Name: "sales", Version: "1.3.0", Author: "Anthropic",
		Skills: []string{"call-prep", "forecast"},
		Servers: []packs.Server{
			{Name: "gmail", Type: "http"},
			{Name: "slack", Type: "http", URL: "https://mcp.slack.com/mcp", OAuth: true},
		},
		Commands: []packs.Command{{Name: "call-prep", Description: "Prep me for a call"}},
	}}
}

func packTool(t *testing.T, resolve func(context.Context, packs.Ref) ([]packs.Pack, error)) *PackTool {
	t.Helper()
	return &PackTool{Resolve: resolve}
}

func callPack(t *testing.T, tool *PackTool, args string) (ToolResult, error) {
	t.Helper()
	ctx := WithToolCallContext(t.Context(), "session", "toolcall", t.TempDir(), 4096)
	return tool.Execute(ctx, json.RawMessage(args))
}

// The tool exists BECAUSE the CLI is unreachable from the sandbox the model runs
// in; it must therefore hand back the same text the operator sees, connectors and
// oauth marker included, or the model is reasoning about a different pack.
func TestPackToolShowRendersTheSameDetailAsTheCLI(t *testing.T) {
	t.Parallel()
	tool := packTool(t, func(_ context.Context, ref packs.Ref) ([]packs.Pack, error) {
		if ref.Directory != "sales" {
			t.Errorf("resolved %+v", ref)
		}
		return packFixture(), nil
	})

	res, err := callPack(t, tool, `{"action":"show","ref":"anthropics/knowledge-work-plugins/sales"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"sales v1.3.0",
		"anthropics/knowledge-work-plugins@call-prep",
		"[needs oauth]",
		"(no endpoint declared",
		"commands (1)",
	} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("result is missing %q:\n%s", want, res.Preview)
		}
	}
}

func TestPackToolListSummarizesARepository(t *testing.T) {
	t.Parallel()
	tool := packTool(t, func(_ context.Context, ref packs.Ref) ([]packs.Pack, error) {
		if ref.Directory != "" {
			t.Errorf("list resolved a directory: %+v", ref)
		}
		return packFixture(), nil
	})

	res, err := callPack(t, tool, `{"action":"list","ref":"anthropics/knowledge-work-plugins"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "1 pack(s)") || !strings.Contains(res.Preview, "sales") {
		t.Errorf("list:\n%s", res.Preview)
	}
}

// `show` on a bare repository is ambiguous. Rendering the first pack would be a
// guess handed to the model as an answer, which is the failure mode this whole
// tool exists to avoid.
func TestPackToolShowRefusesABareRepositoryWithoutResolving(t *testing.T) {
	t.Parallel()
	tool := packTool(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
		t.Fatal("resolved before refusing an ambiguous reference")
		return nil, nil
	})

	_, err := callPack(t, tool, `{"action":"show","ref":"anthropics/knowledge-work-plugins"}`)
	if err == nil || !strings.Contains(err.Error(), "action=list") {
		t.Fatalf("err = %v, want a refusal pointing at list", err)
	}
}

func TestPackToolRejectsBadInputBeforeReachingTheNetwork(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, args string }{
		{"a reference carrying its own host", `{"action":"list","ref":"https://evil.example/x"}`},
		{"a traversal segment", `{"action":"show","ref":"owner/repo/.."}`},
		{"an unknown action", `{"action":"install","ref":"owner/repo"}`},
		{"malformed json", `{"action":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool := packTool(t, func(context.Context, packs.Ref) ([]packs.Pack, error) {
				t.Errorf("%s reached the resolver", tt.name)
				return nil, nil
			})
			if _, err := callPack(t, tool, tt.args); err == nil {
				t.Fatalf("%s was accepted", tt.name)
			}
		})
	}
}

func TestPackToolSurfacesAResolveFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("git clone failed: repository not found")
	tool := packTool(t, func(context.Context, packs.Ref) ([]packs.Pack, error) { return nil, boom })

	if _, err := callPack(t, tool, `{"action":"list","ref":"owner/nope"}`); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the resolver's own failure", err)
	}
}

// Deferred and read-only are both load-bearing: deferred keeps a long description
// out of every turn's manifest, and non-Mutating is what lets the gateway score
// this Safe rather than stopping the turn to read a public repository.
func TestPackToolSpecIsDeferredAndReadOnly(t *testing.T) {
	t.Parallel()
	spec := (&PackTool{}).Spec()
	if spec.Name != "plugin_pack" {
		t.Errorf("name = %q", spec.Name)
	}
	if !spec.Deferred {
		t.Error("plugin_pack is not deferred; its description would ride every turn")
	}
	if spec.Mutating {
		t.Error("plugin_pack is marked Mutating; it only reads a public repository")
	}
	if !json.Valid(spec.Parameters) {
		t.Error("parameter schema is not valid JSON")
	}
}
