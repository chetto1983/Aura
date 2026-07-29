package prompt

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

func testRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	return r
}

func testConfig() llm.Config {
	return llm.Config{
		Provider:    "openrouter",
		Model:       "deepseek/deepseek-v4-flash:exacto",
		BaseURL:     "https://openrouter.ai/api/v1",
		Temperature: 0.7,
		MaxTokens:   4096,
	}
}

func seedHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "first user turn"},
	}
}

func TestBuildPrefixStable(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	t.Run("reproduces the inline construction exactly", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()
		got := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
		want := llm.Request{
			Model:       cfg.Model,
			Messages:    hist,
			Tools:       reg.RenderToolDefs(nil),
			Temperature: cfg.Temperature,
			MaxTokens:   cfg.MaxTokens,
		}
		if got.Model != want.Model || got.Temperature != want.Temperature || got.MaxTokens != want.MaxTokens {
			t.Fatalf("scalar fields drifted: got %+v want %+v", got, want)
		}
		if !reflect.DeepEqual(got.Messages, want.Messages) {
			t.Fatalf("Messages drifted: got %+v want %+v", got.Messages, want.Messages)
		}
		if !reflect.DeepEqual(got.Tools, want.Tools) {
			t.Fatalf("Tools drifted: got %+v want %+v", got.Tools, want.Tools)
		}
		if got.ToolsCacheControl != "" {
			t.Fatalf("openrouter build carried cache_control marker: %q", got.ToolsCacheControl)
		}
	})

	t.Run("messages[0] byte-identical over a growing history", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()
		first := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
		h0, err := PrefixHash(first.Messages, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash: %v", err)
		}
		for turn := range 20 {
			hist = append(hist,
				llm.Message{Role: llm.RoleAssistant, Content: "answer"},
				llm.Message{Role: llm.RoleUser, Content: "next"},
			)
			req := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
			hN, err := PrefixHash(req.Messages, []int{0})
			if err != nil {
				t.Fatalf("PrefixHash turn %d: %v", turn, err)
			}
			if hN != h0 {
				t.Fatalf("messages[0] hash changed at turn %d: %q != %q", turn, hN, h0)
			}
		}
	})

	t.Run("does not re-prepend or mutate history[0]", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()
		before := hist[0]
		req := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
		if len(req.Messages) != len(hist) {
			t.Fatalf("Build changed history length: got %d want %d", len(req.Messages), len(hist))
		}
		if !reflect.DeepEqual(req.Messages[0], before) {
			t.Fatalf("Build mutated history[0]: got %+v want %+v", req.Messages[0], before)
		}
		if !reflect.DeepEqual(hist[0], before) {
			t.Fatalf("Build mutated caller's slice element 0")
		}
	})
}

func TestCacheControlSeam(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	hist := seedHistory()

	t.Run("anthropic sets the ephemeral marker", func(t *testing.T) {
		t.Parallel()
		req := b.Build(hist, reg, "anthropic", cfg, Budget{}, nil)
		if req.ToolsCacheControl == "" {
			t.Fatalf("anthropic build did not set ToolsCacheControl")
		}
	})

	t.Run("openrouter carries no cache_control", func(t *testing.T) {
		t.Parallel()
		req := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
		if req.ToolsCacheControl != "" {
			t.Fatalf("openrouter build carried cache_control: %q", req.ToolsCacheControl)
		}
	})

	t.Run("never marks any history message", func(t *testing.T) {
		t.Parallel()
		req := b.Build(hist, reg, "anthropic", cfg, Budget{}, nil)
		// The seam only touches the tools side (ToolsCacheControl). History messages
		// must remain byte-identical to the input — no per-message cache_control.
		if !reflect.DeepEqual(req.Messages, hist) {
			t.Fatalf("anthropic build mutated history messages: got %+v want %+v", req.Messages, hist)
		}
	})

	t.Run("injectCacheControl is a no-op under openrouter", func(t *testing.T) {
		t.Parallel()
		req := llm.Request{Model: "m", Messages: hist}
		snapshot := req
		injectCacheControl(&req, "openrouter")
		if !reflect.DeepEqual(req, snapshot) {
			t.Fatalf("injectCacheControl mutated request under openrouter: got %+v want %+v", req, snapshot)
		}
	})
}
