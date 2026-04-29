package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"
	"github.com/philippgille/chromem-go"
)

type scenario struct {
	name       string
	prompt     string
	wantTools  []string
	wantText   []string
	rejectText []string
}

func main() {
	liveWeb := flag.Bool("live-web", false, "run real web_search and web_fetch calls with LLM_API_KEY")
	keepWiki := flag.Bool("keep-wiki", false, "keep the temporary wiki directory after the run")
	flag.Parse()

	if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warning: could not load .env: %v\n", err)
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("FAIL: LLM_API_KEY is required")
		os.Exit(1)
	}

	baseURL := envDefault("LLM_BASE_URL", "https://api.openai.com/v1")
	model := envDefault("LLM_MODEL", "gpt-4")
	webBaseURL := envDefault("OLLAMA_WEB_BASE_URL", config.DefaultOllamaWebBaseURL)
	embeddingAPIKey := os.Getenv("EMBEDDING_API_KEY")
	if embeddingAPIKey == "" {
		fmt.Println("FAIL: EMBEDDING_API_KEY is required for wiki search; configure it for Mistral embeddings")
		os.Exit(1)
	}
	embeddingBaseURL := envDefault("EMBEDDING_BASE_URL", "https://api.mistral.ai/v1")
	embeddingModel := envDefault("EMBEDDING_MODEL", "mistral-embed")

	wikiDir, err := os.MkdirTemp("", "aura-debug-tools-*")
	if err != nil {
		fmt.Printf("FAIL: create temp wiki: %v\n", err)
		os.Exit(1)
	}
	if !*keepWiki {
		defer os.RemoveAll(wikiDir)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	store, err := wiki.NewStore(wikiDir, logger)
	if err != nil {
		fmt.Printf("FAIL: create wiki store: %v\n", err)
		os.Exit(1)
	}

	embedFn := createEmbeddingFunc(embeddingAPIKey, embeddingBaseURL, embeddingModel)
	engine, err := search.NewEngine(wikiDir, embedFn, logger)
	if err != nil {
		fmt.Printf("FAIL: create search engine: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if err := seedWiki(ctx, store, engine); err != nil {
		fmt.Printf("FAIL: seed wiki: %v\n", err)
		os.Exit(1)
	}

	reg := tools.NewRegistry(logger)
	reg.Register(tools.NewWriteWikiTool(store, engine))
	reg.Register(tools.NewReadWikiTool(store))
	reg.Register(tools.NewSearchWikiTool(engine))
	if *liveWeb {
		reg.Register(tools.NewWebSearchTool(apiKey, webBaseURL))
		reg.Register(tools.NewWebFetchTool(apiKey, webBaseURL))
	}

	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	scenarios := []scenario{
		{
			name: "write_wiki",
			prompt: "Remember this exact durable fact using the wiki: " +
				"Project Aura natural tool smoke marker is cerulean-731. " +
				"Use the exact title Aura Natural Tool Smoke Marker. " +
				"Save it as a concise wiki page with category debug and tag smoke-test.",
			wantTools: []string{"write_wiki"},
		},
		{
			name:      "read_written_wiki",
			prompt:    "Read the wiki page slug aura-natural-tool-smoke-marker and tell me the marker.",
			wantTools: []string{"read_wiki"},
			wantText:  []string{"cerulean-731"},
		},
		{
			name:      "read_wiki",
			prompt:    "Read the wiki page slug aura-seeded-tool-smoke-marker and tell me the marker.",
			wantTools: []string{"read_wiki"},
			wantText:  []string{"magenta-284"},
		},
		{
			name:      "search_wiki",
			prompt:    "Search the wiki for seeded tool smoke marker magenta and report the saved marker.",
			wantTools: []string{"search_wiki"},
			wantText:  []string{"magenta-284"},
		},
	}
	if *liveWeb {
		scenarios = append(scenarios,
			scenario{
				name:      "web_search",
				prompt:    "Use web search to find the official Ollama web search API documentation. Reply with one source URL.",
				wantTools: []string{"web_search"},
				wantText:  []string{"docs.ollama.com"},
			},
			scenario{
				name:      "web_fetch",
				prompt:    "Fetch https://docs.ollama.com/capabilities/web-search and summarize the web_search endpoint in one sentence.",
				wantTools: []string{"web_fetch"},
				wantText:  []string{"web_search"},
			},
		)
	}

	fmt.Printf("Natural tool smoke test\n")
	fmt.Printf("model=%s base_url=%s live_web=%v wiki=%s\n", model, baseURL, *liveWeb, wikiDir)
	fmt.Printf("llm_api_key=SET\n")
	fmt.Printf("web_api_key=LLM_API_KEY\n")
	fmt.Printf("embedding_base_url=%s embedding_model=%s\n\n", embeddingBaseURL, embeddingModel)

	failures := 0
	for _, sc := range scenarios {
		called, final, toolResults, err := runScenario(ctx, client, reg, model, sc.prompt)
		text := final + "\n" + strings.Join(toolResults, "\n")
		ok := err == nil &&
			containsTools(called, sc.wantTools) &&
			containsAllText(text, sc.wantText) &&
			containsNoText(text, append(sc.rejectText, "(tool error)"))

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
			fmt.Printf("  final: %s\n", singleLine(final, 220))
		}
		fmt.Println()
	}

	if *keepWiki {
		fmt.Printf("kept wiki: %s\n", wikiDir)
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

func seedWiki(ctx context.Context, store *wiki.Store, engine *search.Engine) error {
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         "Aura Seeded Tool Smoke Marker",
		Body:          "The seeded natural tool smoke marker is magenta-284.",
		Tags:          []string{"smoke-test"},
		Category:      "debug",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.WritePage(ctx, page); err != nil {
		return err
	}
	return engine.ReindexWikiPage(ctx, "aura-seeded-tool-smoke-marker")
}

func createEmbeddingFunc(apiKey, baseURL, model string) chromem.EmbeddingFunc {
	normalized := true
	return chromem.NewEmbeddingFuncOpenAICompat(baseURL, apiKey, model, &normalized)
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func containsTools(called, wants []string) bool {
	seen := make(map[string]bool, len(called))
	for _, name := range called {
		seen[name] = true
	}
	for _, want := range wants {
		if !seen[want] {
			return false
		}
	}
	return true
}

func containsAllText(text string, wants []string) bool {
	text = strings.ToLower(text)
	for _, want := range wants {
		if !strings.Contains(text, strings.ToLower(want)) {
			return false
		}
	}
	return true
}

func containsNoText(text string, rejects []string) bool {
	text = strings.ToLower(text)
	for _, reject := range rejects {
		if strings.TrimSpace(reject) == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(reject)) {
			return false
		}
	}
	return true
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
