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
//	go run ./cmd/probe_chat                       # run all cases
//	go run ./cmd/probe_chat -case schedule        # run one case
//	go run ./cmd/probe_chat -prompt "..." -raw    # ad-hoc one-shot
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
	"strings"
	"time"
)

func main() {
	var (
		caseName = flag.String("case", "", "run only the named case (empty = run all)")
		prompt   = flag.String("prompt", "", "send a single ad-hoc prompt and print the structured reply (skips Verify)")
		jsonOut  = flag.Bool("json", false, "emit results as JSON instead of human-readable table")
		baseURL  = flag.String("url", envDefault("AURA_CHAT_URL", "http://localhost:18080/api/chat"), "chat endpoint")
		apiBase  = flag.String("api", envDefault("AURA_API_BASE", "http://localhost:18080/api"), "dashboard API base (used for wiki ground truth)")
		token    = flag.String("token", os.Getenv("AURA_CHAT_TOKEN"), "bearer token (defaults to $AURA_CHAT_TOKEN)")
		dbPath   = flag.String("db", envDefault("AURA_DB_PATH", "./data/aura.db"), "SQLite DB path (read-only)")
		timeoutS = flag.Int("timeout", 240, "per-prompt timeout (seconds)")
	)
	flag.Parse()

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
	defer db.Close()
	env := &Env{
		DB:        db,
		DBPath:    *dbPath,
		APIBase:   *apiBase,
		APIToken:  *token,
		APIClient: client,
	}

	cases := allCases(time.Now())
	if *caseName != "" {
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
