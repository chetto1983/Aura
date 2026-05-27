// Command probe_chat is the canonical end-to-end chat pipe for Aura.
//
// It exercises the running /api/chat endpoint with a battery of cases
// and verifies BOTH the assistant's textual reply AND ground truth
// (SQLite tables, filesystem). This is the rule documented in
// CLAUDE.md: "tool_calls: N in the response is necessary but never
// sufficient; the model can call a tool correctly and still
// hallucinate around the result."
//
// Roles per CLAUDE.md:
//
//	I (Claude Code) am the programmer — I write and run this pipe.
//	Aura is the runtime under test — every textual claim she makes
//	must be cross-checked against ground truth before being trusted.
//
// Usage:
//
//	go run ./cmd/probe_chat                            # run all cases
//	go run ./cmd/probe_chat -case schedule             # run one case by name
//	go run ./cmd/probe_chat -smoke=markitdown          # SMOKE TIER: run a category subset (≤8)
//	go run ./cmd/probe_chat -prompt "..." -raw         # ad-hoc one-shot
//
// Valid -smoke categories (from qa-coverage-tools.md / qa-coverage-channel-failure.md):
//
//	tools-files          doc-xlsx, doc-docx, doc-pdf, file-write-read, xlsx-italian-chars
//	tools-skills         natural-skill-authoring
//	tools-memory         wiki-page-create, phase07d-mixed-tier-recall, phase07f-wiki-frontmatter
//	tools-source         source-store-read-roundtrip, phase07e-source-span-read
//	tools-swarm          natural-multiagent-orchestrator-scratchpad, natural-multiagent-no-overdelegation
//	tools-web            web-fetch-summarize-*
//	tools-scheduler      schedule-reminder
//	tools-agent-note     agent-note-roundtrip
//	channels-web         greeting-no-tools
//	channels-telegram    (no cases yet)
//	failure-modes-phantom phantom-trap-nonexistent-task
//	failure-modes-budget  (no cases yet)
//	markitdown           markitdown-xlsx/docx/pptx/epub/html/zip-extract
//
// Env config (with defaults):
//
//	AURA_CHAT_URL    = http://localhost:18080/api/chat
//	AURA_CHAT_TOKEN  = (required for non-loopback; bearer token)
//	AURA_DB_PATH     = ./data/aura.db   (read-only)
//	AURA_API_BASE    = http://localhost:18080/api   (for wiki ground truth)
//
// Wiki ground truth is fetched via /api/wiki/page?slug=… because the
// in-container wiki lives in a named Docker volume, not a bind mount.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// validSmokeCategories is the exhaustive list of smoke tier categories.
// Each maps to a set of Case.Category values in cases.go / phase07*.go.
var validSmokeCategories = []string{
	"channels-telegram",
	"channels-web",
	"failure-modes-budget",
	"failure-modes-phantom",
	"markitdown",
	"tools-agent-note",
	"tools-files",
	"tools-mcp",
	"tools-dev",
	"tools-memory",
	"tools-sandbox",
	"tools-scheduler",
	"tools-skills",
	"tools-source",
	"tools-swarm",
	"tools-registry",
	"tools-web",
}

func main() {
	var (
		caseName  = flag.String("case", "", "run only the named case (empty = run all)")
		smoke     = flag.String("smoke", "", "SMOKE TIER: run only cases in this category (see usage for valid values; mutually exclusive with -case)")
		prompt    = flag.String("prompt", "", "send a single ad-hoc prompt and print the structured reply (skips Verify)")
		jsonOut   = flag.Bool("json", false, "emit results as JSON instead of human-readable table")
		baseURL   = flag.String("url", envDefault("AURA_CHAT_URL", "http://localhost:18080/api/chat"), "chat endpoint")
		apiBase   = flag.String("api", envDefault("AURA_API_BASE", "http://localhost:18080/api"), "dashboard API base (used for wiki ground truth)")
		token     = flag.String("token", os.Getenv("AURA_CHAT_TOKEN"), "bearer token (defaults to $AURA_CHAT_TOKEN)")
		dbPath    = flag.String("db", envDefault("AURA_DB_PATH", "./data/aura.db"), "SQLite DB path (read-only)")
		timeoutS  = flag.Int("timeout", 240, "per-prompt timeout (seconds)")
		enableTTS = flag.Bool("tts", false, "include TTS probe cases (requires AURA_POCKETTTS_URL and pocket-tts sidecar up)")
	)
	flag.Parse()

	if *smoke != "" && *caseName != "" {
		fail("-smoke and -case are mutually exclusive")
	}

	// Validate smoke category early — before token check — so bad category names
	// get a helpful message even when AURA_CHAT_TOKEN is unset.
	if *smoke != "" && !isSmokeCategory(*smoke) {
		fmt.Fprintf(os.Stderr, "error: unknown smoke category %q\nvalid categories: %s\n",
			*smoke, strings.Join(validSmokeCategories, ", "))
		os.Exit(1)
	}

	if *caseName == promptHealthCaseName {
		db, err := openReadOnly(*dbPath)
		if err != nil {
			fail(fmt.Sprintf("open DB %s: %v", *dbPath, err))
		}
		defer closeProbeDB(db)
		env := &Env{
			DB:       db,
			DBPath:   *dbPath,
			APIBase:  *apiBase,
			APIToken: *token,
		}
		if err := runPromptHealthCase(env, *jsonOut); err != nil {
			fail(err.Error())
		}
		return
	}

	if *token == "" {
		fail("AURA_CHAT_TOKEN is required (env or -token)")
	}

	client := &http.Client{Timeout: time.Duration(*timeoutS) * time.Second}

	// Ad-hoc one-shot: print structured reply, exit.
	if strings.TrimSpace(*prompt) != "" {
		reply, err := sendChat(client, *baseURL, *token, *prompt)
		if err != nil {
			fail(err.Error())
		}
		printOneShot(reply, *jsonOut)
		return
	}

	// Ground-truth probes need DB + wiki access.
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fail(fmt.Sprintf("open DB %s: %v", *dbPath, err))
	}
	defer closeProbeDB(db)
	env := &Env{
		DB:        db,
		DBPath:    *dbPath,
		APIBase:   *apiBase,
		APIToken:  *token,
		APIClient: client,
	}

	cases := allCases(time.Now(), *enableTTS)

	switch {
	case *caseName != "":
		filtered := cases[:0]
		for _, c := range cases {
			if c.Name == *caseName {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			fail(fmt.Sprintf("no case named %q", *caseName))
		}
		cases = filtered

	case *smoke != "":
		var filtered []Case
		for _, c := range cases {
			if c.Category == *smoke {
				filtered = append(filtered, c)
			}
		}
		// Sort alphabetically by name for determinism, then cap at 8.
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Name < filtered[j].Name })
		if len(filtered) > 8 {
			fmt.Fprintf(os.Stderr, "warning: category %q has %d cases; running first 8 alphabetically\n",
				*smoke, len(filtered))
			filtered = filtered[:8]
		}
		cases = filtered
	}

	results := runAll(client, *baseURL, *token, env, cases)
	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(results)
	} else {
		printReport(results)
	}
	if anyFailed(results) {
		os.Exit(1)
	}
}

func isSmokeCategory(s string) bool {
	for _, v := range validSmokeCategories {
		if v == s {
			return true
		}
	}
	return false
}

func closeProbeDB(db interface{ Close() error }) {
	if err := db.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: close DB: %v\n", err)
	}
}
