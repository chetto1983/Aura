package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/cmd/debug_common"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/workspace"
)

type scenario struct {
	name       string
	prompt     string
	wantTools  []string
	wantText   []string
	rejectText []string
}

func main() {
	liveWeb := flag.Bool("live-web", false, "run real web action=search/fetch calls with WEB_SEARCH_PROVIDER")
	var keepWorkspace bool
	flag.BoolVar(&keepWorkspace, "keep-workspace", false, "keep the temporary workspace directory after the run")
	flag.Parse()

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("FAIL: LLM_API_KEY is required")
		os.Exit(1)
	}

	baseURL := debugcommon.EnvDefault("LLM_BASE_URL", "https://api.openai.com/v1")
	model := debugcommon.EnvDefault("LLM_MODEL", "gpt-4")
	webSearchProvider := strings.ToLower(debugcommon.EnvDefault("WEB_SEARCH_PROVIDER", "disabled"))
	searxngBaseURL := debugcommon.EnvDefault("SEARXNG_BASE_URL", config.DefaultSearXNGBaseURL)

	workspaceDir, err := os.MkdirTemp("", "aura-debug-tools-*")
	if err != nil {
		fmt.Printf("FAIL: create temp workspace: %v\n", err)
		os.Exit(1)
	}
	if !keepWorkspace {
		defer os.RemoveAll(workspaceDir)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	root, err := workspace.New(workspaceDir)
	if err != nil {
		fmt.Printf("FAIL: create workspace root: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if err := seedWorkspace(workspaceDir); err != nil {
		fmt.Printf("FAIL: seed workspace: %v\n", err)
		os.Exit(1)
	}

	reg := tools.NewRegistry(logger)
	reg.Register(tools.NewFileTool(root))
	if *liveWeb {
		switch webSearchProvider {
		case "searxng":
			reg.Register(tools.NewWebTool(searxngBaseURL))
		default:
			fmt.Println("FAIL: set WEB_SEARCH_PROVIDER=searxng when using -live-web")
			os.Exit(1)
		}
	}

	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	scenarios := []scenario{
		{
			name: "file_write",
			prompt: "Remember this exact durable fact using file action=write at notes/aura-natural-tool-smoke-marker.md: " +
				"Project Aura natural tool smoke marker is cerulean-731. " +
				"Use a concise markdown note with title Aura Natural Tool Smoke Marker.",
			wantTools: []string{"file"},
		},
		{
			name:      "file_read_written",
			prompt:    "Use file action=read to read notes/aura-natural-tool-smoke-marker.md and tell me the marker.",
			wantTools: []string{"file"},
			wantText:  []string{"cerulean-731"},
		},
		{
			name:      "file_read_seeded",
			prompt:    "Use file action=read to read notes/seeded-tool-smoke-marker.md and tell me the marker.",
			wantTools: []string{"file"},
			wantText:  []string{"magenta-284"},
		},
		{
			name:      "file_search",
			prompt:    "Use file action=search to search markdown notes for seeded tool smoke marker magenta and report the saved marker.",
			wantTools: []string{"file"},
			wantText:  []string{"magenta-284"},
		},
		{
			name:      "file_patch",
			prompt:    "Use file action=patch to replace magenta-284 with magenta-285 in notes/seeded-tool-smoke-marker.md, then file action=read to report the updated marker.",
			wantTools: []string{"file"},
			wantText:  []string{"magenta-285"},
		},
		{
			name:      "file_list",
			prompt:    "Use file action=list to list the notes directory and tell me which smoke marker files are there.",
			wantTools: []string{"file"},
			wantText:  []string{"seeded-tool-smoke-marker.md", "aura-natural-tool-smoke-marker.md"},
		},
	}
	if *liveWeb {
		scenarios = append(scenarios,
			scenario{
				name:      "web_action_search",
				prompt:    "Use web action=search to find the official SearXNG search API documentation. Reply with one source URL.",
				wantTools: []string{"web"},
				wantText:  []string{"searxng"},
			},
			scenario{
				name:      "web_action_fetch",
				prompt:    "Use web action=fetch on https://docs.searxng.org/dev/search_api.html and summarize the SearXNG JSON search API in one sentence.",
				wantTools: []string{"web"},
				wantText:  []string{"SearXNG"},
			},
		)
	}

	fmt.Printf("Natural tool smoke test\n")
	fmt.Printf("model=%s base_url=%s live_web=%v workspace=%s\n", model, baseURL, *liveWeb, workspaceDir)
	fmt.Printf("llm_api_key=SET\n")
	fmt.Printf("web_provider=%s searxng_base_url=%s\n", webSearchProvider, searxngBaseURL)
	fmt.Println()

	failures := 0
	for _, sc := range scenarios {
		called, final, toolResults, err := runScenario(ctx, client, reg, model, sc.prompt)
		text := final + "\n" + strings.Join(toolResults, "\n")
		ok := err == nil &&
			debugcommon.ContainsTools(called, sc.wantTools) &&
			debugcommon.ContainsAllText(text, sc.wantText) &&
			debugcommon.ContainsNoText(text, append(sc.rejectText, "(tool error)"))

		status := "PASS"
		if !ok {
			status = "FAIL"
			failures++
		}
		fmt.Printf("[%s] %s\n", status, sc.name)
		fmt.Printf("  wanted: %s\n", strings.Join(sc.wantTools, ", "))
		fmt.Printf("  called: %s\n", strings.Join(called, ", "))
		if len(sc.wantText) > 0 {
			fmt.Printf("  expected text: %s\n", strings.Join(sc.wantText, ", "))
		}
		if err != nil {
			fmt.Printf("  error: %v\n", err)
		}
		if final != "" {
			fmt.Printf("  final: %s\n", debugcommon.SingleLine(final, 220))
		}
		fmt.Println()
	}

	if keepWorkspace {
		fmt.Printf("kept workspace: %s\n", workspaceDir)
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func runScenario(ctx context.Context, client llm.Client, reg *tools.Registry, model, prompt string) ([]string, string, []string, error) {
	messages := []llm.Message{
		{Role: "system", Content: conversation.DefaultSystemPrompt() + "\n\nFor this smoke test, use the relevant tool. Do not answer from memory when a tool can be used."},
		{Role: "user", Content: prompt},
	}

	var called []string
	var toolResults []string
	var lastToolResult string
	for i := 0; i < 8; i++ {
		resp, err := client.Send(ctx, llm.Request{
			Messages:    messages,
			Model:       model,
			Temperature: llm.Float64Ptr(0),
			Tools:       reg.Definitions(),
		})
		if err != nil {
			return called, "", toolResults, err
		}
		if !resp.HasToolCalls {
			final := strings.TrimSpace(resp.Content)
			if final == "" {
				final = lastToolResult
			}
			return called, final, toolResults, nil
		}

		messages = append(messages, llm.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			called = append(called, tc.Name)
			result, err := reg.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = "(tool error) " + err.Error()
			}
			lastToolResult = result
			toolResults = append(toolResults, result)
			messages = append(messages, llm.Message{Role: "tool", Content: result, ToolCallID: tc.ID})
		}
	}
	return called, lastToolResult, toolResults, fmt.Errorf("max tool iterations reached")
}

func seedWorkspace(root string) error {
	notesDir := filepath.Join(root, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return err
	}
	content := "# Aura Seeded Tool Smoke Marker\n\nThe seeded natural tool smoke marker is magenta-284.\n"
	return os.WriteFile(filepath.Join(notesDir, "seeded-tool-smoke-marker.md"), []byte(content), 0o644)
}
