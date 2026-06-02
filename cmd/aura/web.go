package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/web"
)

// runWeb is the `aura web` entry point (D-43/D-44/D-45). It is a hand-parsed
// switch (no cobra — repo convention) over `doctor` and `tool web_search|web_fetch
// '<json>'`. The subcommand name is `web` so the `tool` verb (singular) never
// collides with the existing top-level `tools` (plural) manifest printer. Output
// is human-readable only (no JSON operator output, D-45); every path exits with a
// sysexits code (exec.go: 70 unreachable / 71 infra / 64 usage). It NEVER falls
// back to a public SearXNG instance (D-05/D-44) — SEARXNG_URL is the only backend.
func runWeb(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: aura web {doctor|tool <web_search|web_fetch> '<json args>'}")
		os.Exit(exitUsage)
	}
	switch args[0] {
	case "doctor":
		runWebDoctor()
	case "tool":
		runWebTool(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "aura web: unknown subcommand %q (want doctor|tool)\n", args[0])
		os.Exit(exitUsage)
	}
}

// runWebDoctor checks the web stack the way an operator would (D-44): SEARXNG_URL
// set → reachable + JSON round-trip → a tiny live search succeeds. It uses
// config.LoadDB() so it does NOT require OPENROUTER_API_KEY. Distinct exit codes:
// 64 when SEARXNG_URL is unset (not configured), 70 when the backend is
// unreachable / not returning JSON, 0 on a clean round-trip. No public fallback.
func runWebDoctor() {
	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.SearxngURL) == "" {
		fmt.Fprintln(os.Stderr, "aura web doctor: SEARXNG_URL is not configured — set it to the in-network SearXNG /search endpoint (no public fallback)")
		os.Exit(exitUsage)
	}
	fmt.Printf("SEARXNG_URL: %s\n", cfg.SearxngURL)

	client := web.NewClient(cfg)
	ctx := context.Background()

	// A tiny live search is the round-trip probe: it exercises reachability + the
	// JSON format contract + result parsing in one shot.
	results, err := client.Search(ctx, web.SearchParams{Query: "aura web doctor probe", MaxResults: 1})
	if err != nil {
		// A *web.WebError here is the sanitized unavailable/unreachable signal — print
		// its safe headline (never an IP/host) and exit unreachable.
		fmt.Fprintf(os.Stderr, "aura web doctor: SearXNG probe failed: %s\n", err.Error())
		os.Exit(exitUnreachable)
	}
	fmt.Printf("reachable: yes (JSON round-trip OK; probe returned %d result(s))\n", len(results))
	fmt.Println("status: OK")
	os.Exit(0)
}

// runWebTool runs `aura web tool <web_search|web_fetch> '<json>'`: it marshals the
// JSON arg through the SAME web.Client the agent uses and prints the human-readable
// result (D-43). It is the operator/CI smoke surface for SC#1/SC#2. A *web.WebError
// is printed as its sanitized headline + JSON and is NOT a hard infra failure (the
// model-visible block is a normal outcome); a parse/usage error is 64, an infra
// fault is 71.
func runWebTool(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aura web tool <web_search|web_fetch> '<json args>'")
		os.Exit(exitUsage)
	}
	name, raw := args[0], args[1]
	cfg := config.LoadDB()
	client := web.NewClient(cfg)
	ctx := context.Background()

	switch name {
	case "web_search":
		runWebToolSearch(ctx, client, raw)
	case "web_fetch":
		runWebToolFetch(ctx, client, raw)
	default:
		fmt.Fprintf(os.Stderr, "aura web tool: unknown tool %q (want web_search|web_fetch)\n", name)
		os.Exit(exitUsage)
	}
}

func runWebToolSearch(ctx context.Context, client *web.Client, raw string) {
	var p struct {
		Query           string   `json:"query"`
		MaxResults      int      `json:"max_results"`
		Category        string   `json:"category"`
		Language        string   `json:"language"`
		TimeRange       string   `json:"time_range"`
		Domains         []string `json:"domains"`
		IncludeMetadata bool     `json:"include_metadata"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		fmt.Fprintln(os.Stderr, "aura web tool web_search: invalid JSON args:", err)
		os.Exit(exitUsage)
	}
	results, err := client.Search(ctx, web.SearchParams{
		Query:           p.Query,
		MaxResults:      p.MaxResults,
		Category:        p.Category,
		Language:        p.Language,
		TimeRange:       p.TimeRange,
		Domains:         p.Domains,
		IncludeMetadata: p.IncludeMetadata,
	})
	if err != nil {
		printWebError("web_search", err)
		return
	}
	fmt.Printf("%d result(s):\n", len(results))
	for i, r := range results {
		fmt.Printf("%2d. %s\n    %s\n    %s\n", i+1, r.Title, r.URL, r.Snippet)
	}
}

func runWebToolFetch(ctx context.Context, client *web.Client, raw string) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		fmt.Fprintln(os.Stderr, "aura web tool web_fetch: invalid JSON args:", err)
		os.Exit(exitUsage)
	}
	page, err := client.Fetch(ctx, "aura-web-cli", p.URL)
	if err != nil {
		printWebError("web_fetch", err)
		return
	}
	fmt.Printf("title: %s\nurl: %s\n", page.Title, page.URL)
	if page.Warning != "" {
		fmt.Printf("warning: %s\n", page.Warning)
	}
	fmt.Printf("links: %d\n---\n%s\n", len(page.Links), page.ContentMD)
}

// printWebError renders the SANITIZED model-visible error to the operator. The
// engine already strips every IP/host/header/redirect-chain (D-27); the CLI just
// surfaces the safe {error,reason,message} JSON. A web block is a normal outcome,
// so the process exits 0 — the operator sees the structured reason, not an infra
// crash. Anything that is NOT a *web.WebError is a genuine infra fault → 71.
func printWebError(tool string, err error) {
	we, ok := web.AsWebError(err)
	if !ok {
		fmt.Fprintf(os.Stderr, "aura web tool %s: %v\n", tool, err)
		os.Exit(exitInfra)
	}
	b, mErr := we.JSON()
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "aura web tool %s: %s\n", tool, we.Error())
		os.Exit(exitInfra)
	}
	fmt.Printf("%s blocked/unavailable: %s\n", tool, string(b))
}
