// Hidden `aura cache-audit` — the runtime-faithful KV-cache prefix invariant gate
// (D-04/D-05/D-06, SC#1/SC#5). It replays 20 deterministic fixtures through the
// REAL runner.Turn -> LlmAgent.Run -> PromptBuilder.Build path against an
// agenttest.FakeClient (no synthetic Build() shortcut), reads each captured
// Requests[n].Messages[0], hashes it with prompt.PrefixHash({0}), prints
// `request NN: <hex>` to stdout, and asserts every hash is identical. The whole audit
// runs against in-memory fake Stores, so it needs NO Postgres.
//
// Exit codes (PRD amendment #16): 0 pass / 1 messages[0] mutation / 2 fixture corrupt.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

const (
	exitMutation = 1 // messages[0] drifted between turns
	exitFixture  = 2 // a fixture file is missing / unparseable

	auditTurns      = 20
	auditFixtureDir = "scripts/fixtures/cache_invariant"
)

// fixtureResponse is one scripted FakeClient turn. Exactly one of {Text, ToolCalls}
// is populated: a Text response streams content + Finish; a ToolCalls response emits
// finalized tool calls then a "tool_calls" finish (the agent threads the results and
// consumes the NEXT response in the same Runner.Turn).
type fixtureResponse struct {
	Text      string            `json:"text,omitempty"`
	Finish    string            `json:"finish,omitempty"`
	ToolCalls []fixtureToolCall `json:"tool_calls,omitempty"`
}

// fixtureToolCall is a finalized tool call in a fixture (id + name + raw JSON args).
type fixtureToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// fixtureTurn is one replay turn: the user message plus the ordered FakeClient
// responses the scripted round consumes (a tool round needs ≥2 responses).
type fixtureTurn struct {
	User      string            `json:"user"`
	Responses []fixtureResponse `json:"responses"`
}

// runCacheAudit is the hidden `aura cache-audit` entry point (the prefix gate).
func runCacheAudit(args []string) {
	os.Exit(cacheAuditMain(context.Background(), args, os.Stdout, os.Stderr))
}

// cacheAuditMain is the testable core: it loads + replays the fixtures and returns
// a process exit code. fakeAuditClient lets the negative test inject a mutation.
func cacheAuditMain(ctx context.Context, _ []string, out, errOut io.Writer) int {
	dir := auditFixtureDir
	root, err := repoRoot()
	if err != nil {
		// A repo-root failure is a genuine environment problem (invoked from a
		// cwd with no go.mod above it). Surface it before falling back to the
		// relative fixture path so the operator sees the real cause instead of a
		// misleading "fixture corrupt" downstream (WR-05).
		_, _ = fmt.Fprintf(errOut, "cache-audit: could not locate repo root (go.mod) from cwd: %v — falling back to relative fixture dir %q\n", err, auditFixtureDir)
	} else {
		dir = filepath.Join(root, auditFixtureDir)
	}
	turns, code := loadFixtures(dir, errOut)
	if code != 0 {
		return code
	}
	reqs, code := replayAudit(ctx, turns, errOut)
	if code != 0 {
		return code
	}
	return reportHashes(reqs, out, errOut)
}

// reportHashes prints `request NN: <hex>` for each request's messages[0] and asserts
// every hash is identical. The first drift returns exitMutation with the SC#5
// wording; this is the seam the Go-level negative test drives directly.
func reportHashes(reqs []llm.Request, out, errOut io.Writer) int {
	prev, prevSet := "", false
	for i, req := range reqs {
		h, err := hashMessages0(req)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "cache-audit: request %02d hash: %v\n", i+1, err)
			return exitFixture
		}
		_, _ = fmt.Fprintf(out, "request %02d: %s\n", i+1, h)
		if prevSet && h != prev {
			_, _ = fmt.Fprintf(errOut, "messages[0] mutated at request %d -- diff: %s vs %s\n", i+1, prev, h)
			return exitMutation
		}
		prev, prevSet = h, true
	}
	return 0
}

// replayAudit drives the real Runner.Turn loop turn-by-turn and returns every
// captured request each fixture turn emits. Tool rounds can consume multiple LLM
// requests inside one Runner.Turn, and each one must preserve messages[0].
func replayAudit(ctx context.Context, turns []fixtureTurn, errOut io.Writer) ([]llm.Request, int) {
	// A throwaway run dir keeps any tool-result spillover (e.g. the tool_search
	// round) out of the cwd; it is removed when the replay returns.
	runDir, err := os.MkdirTemp("", "aura-cache-audit-")
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "cache-audit: temp run dir:", err)
		return nil, exitFixture
	}
	defer func() { _ = os.RemoveAll(runDir) }()

	client := agenttest.NewFakeClient(scriptTurns(turns)...)
	r := runner.New(runner.Deps{
		Conv:         newMemConvStore(),
		Pause:        newMemPauseStore(),
		Identity:     memIdentityStore{},
		CacheMetrics: memCacheMetricStore{},
		Client:       client,
		Registry:     buildRegistry(),
		LLM:          llm.Config{Model: "cache-audit", ContextWindow: 1_000_000, MaxOutputTokens: 32768},
		RunDir:       runDir,
	})

	convID := "00000000-0000-0000-0000-0000000000aa"
	if _, err := r.NewConversationWithID(ctx, convID); err != nil {
		_, _ = fmt.Fprintln(errOut, "cache-audit: create conversation:", err)
		return nil, exitFixture
	}

	reqs := make([]llm.Request, 0, expectedAuditRequests(turns))
	for i := range turns {
		before := len(client.Requests)
		user := turns[i].User
		if err := drainTurn(r.Turn(ctx, convID, &user)); err != nil {
			_, _ = fmt.Fprintf(errOut, "cache-audit: turn %02d replay: %v\n", i+1, err)
			return nil, exitFixture
		}
		if len(client.Requests) == before {
			_, _ = fmt.Fprintf(errOut, "cache-audit: turn %02d replay emitted no LLM requests\n", i+1)
			return nil, exitFixture
		}
		reqs = append(reqs, client.Requests[before:]...)
	}

	_ = r.Stop(ctx, convID)
	return reqs, 0
}

func expectedAuditRequests(turns []fixtureTurn) int {
	n := 0
	for _, t := range turns {
		n += len(t.Responses)
	}
	return n
}

// hashMessages0 fingerprints req.Messages[0] with the forward-compatible {0} index
// set (D-06a) — the same hash the production CI gate reads.
func hashMessages0(req llm.Request) (string, error) {
	return prompt.PrefixHash(req.Messages, []int{0})
}

// drainTurn runs a Runner.Turn iterator to completion, returning the first error.
func drainTurn(seq iter.Seq2[*agent.Event, error]) error {
	var firstErr error
	for _, err := range seq {
		if err != nil {
			firstErr = err
			break
		}
	}
	return firstErr
}

// decodeFixture strictly parses one fixture file (unknown fields are rejected so a
// malformed fixture is exit 2, never a silent partial parse).
func decodeFixture(raw []byte) (fixtureTurn, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var ft fixtureTurn
	if err := dec.Decode(&ft); err != nil {
		return fixtureTurn{}, err
	}
	if ft.User == "" {
		return fixtureTurn{}, fmt.Errorf("fixture has empty user message")
	}
	if len(ft.Responses) == 0 {
		return fixtureTurn{}, fmt.Errorf("fixture has no scripted responses")
	}
	return ft, nil
}

// scriptTurns flattens the fixtures into the ordered FakeClient turn script. A
// text response becomes a TextChunks turn; a tool-call response becomes a
// ToolCallTurn — exactly the builders the runner tests use.
func scriptTurns(turns []fixtureTurn) []agenttest.FakeTurn {
	var script []agenttest.FakeTurn
	for _, t := range turns {
		for _, resp := range t.Responses {
			script = append(script, toFakeTurn(resp))
		}
	}
	return script
}

func toFakeTurn(resp fixtureResponse) agenttest.FakeTurn {
	if len(resp.ToolCalls) > 0 {
		calls := make([]llm.ToolCall, 0, len(resp.ToolCalls))
		for _, c := range resp.ToolCalls {
			calls = append(calls, agenttest.MakeToolCall(c.ID, c.Name, c.Arguments))
		}
		return agenttest.ToolCallTurn(calls...)
	}
	finish := resp.Finish
	if finish == "" {
		finish = "stop"
	}
	return agenttest.TextChunks(finish, resp.Text)
}

// loadFixtures reads turn-01.json..turn-20.json in order. A missing or unparseable
// fixture is exit 2 (fixture corrupt) — never a silent pass.
func loadFixtures(dir string, errOut io.Writer) ([]fixtureTurn, int) {
	turns := make([]fixtureTurn, 0, auditTurns)
	for i := 1; i <= auditTurns; i++ {
		path := filepath.Join(dir, fmt.Sprintf("turn-%02d.json", i))
		raw, err := os.ReadFile(path) //nolint:gosec // fixed in-repo fixture path
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "cache-audit: read fixture %s: %v\n", path, err)
			return nil, exitFixture
		}
		ft, derr := decodeFixture(raw)
		if derr != nil {
			_, _ = fmt.Fprintf(errOut, "cache-audit: parse fixture %s: %v\n", path, derr)
			return nil, exitFixture
		}
		turns = append(turns, ft)
	}
	return turns, 0
}

// repoRoot walks up from the cwd to the dir holding go.mod so the audit finds the
// fixtures whether invoked from the repo root (CI) or a subdir.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
