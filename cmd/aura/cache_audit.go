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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/canonicaljson"
	"github.com/chetto1983/aura/internal/config"
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
// every hash is identical. When a request ALSO carries a messages[1] always-block (D-07)
// and/or the non-deferred skill tool's manifest-in-Description (D-06) it prints
// `messages1 NN: <hex>` / `skillman NN: <hex>` and asserts those streams are byte-stable
// too. The messages[0] invariant is checked FIRST and is unconditional (the original
// CAP-04 gate); the messages[1]/skill streams are emitted only when present, so a
// synthetic request list with no skills (the Go-level negative test) still exercises the
// messages[0] mutation path. The real 20-turn replay wires a fixed skill set, so all
// three streams ARE present and the script's per-stream 22-line count enforces it
// (no-skip-as-green). The first drift in ANY stream returns exitMutation with the
// matching wording; this is the seam the Go-level negative test drives directly.
func reportHashes(reqs []llm.Request, out, errOut io.Writer) int {
	prev0, set0 := "", false
	prev1, set1 := "", false
	prevMan, setMan := "", false
	for i, req := range reqs {
		// messages[0] — unconditional (the original CAP-04 invariant). Checked first so a
		// drift here is reported before any optional-stream fixture error.
		h0, err := hashMessages0(req)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "cache-audit: request %02d hash: %v\n", i+1, err)
			return exitFixture
		}
		_, _ = fmt.Fprintf(out, "request %02d: %s\n", i+1, h0)
		if set0 && h0 != prev0 {
			_, _ = fmt.Fprintf(errOut, "messages[0] mutated at request %d -- diff: %s vs %s\n", i+1, prev0, h0)
			return exitMutation
		}
		prev0, set0 = h0, true

		// messages[1] always-block (D-07) — only when the request carries one.
		if len(req.Messages) >= 2 {
			h1, herr := hashMessages1(req)
			if herr != nil {
				_, _ = fmt.Fprintf(errOut, "cache-audit: request %02d messages[1] hash: %v\n", i+1, herr)
				return exitFixture
			}
			_, _ = fmt.Fprintf(out, "messages1 %02d: %s\n", i+1, h1)
			if set1 && h1 != prev1 {
				_, _ = fmt.Fprintf(errOut, "messages[1] always-block mutated at request %d -- diff: %s vs %s\n", i+1, prev1, h1)
				return exitMutation
			}
			prev1, set1 = h1, true
		}

		// skill manifest-in-Description (D-06) — only when the non-deferred skill tool is
		// present in this request's tools.
		if hMan, ok := skillManifestHash(req); ok {
			_, _ = fmt.Fprintf(out, "skillman %02d: %s\n", i+1, hMan)
			if setMan && hMan != prevMan {
				_, _ = fmt.Fprintf(errOut, "skill manifest-in-Description mutated at request %d -- diff: %s vs %s\n", i+1, prevMan, hMan)
				return exitMutation
			}
			prevMan, setMan = hMan, true
		}
	}
	return 0
}

// replayAudit drives the real Runner.Turn loop turn-by-turn and returns every
// captured request each fixture turn emits. Tool rounds can consume multiple LLM
// requests inside one Runner.Turn, and each one must preserve messages[0] AND — with
// a fixed skill set loaded — the messages[1] always-block + the skill tool's
// manifest-in-Description (Phase 11 D-06/D-07 extension).
func replayAudit(ctx context.Context, turns []fixtureTurn, errOut io.Writer) ([]llm.Request, int) {
	// A throwaway run dir keeps any tool-result spillover (e.g. the tool_search
	// round) out of the cwd; it is removed when the replay returns.
	runDir, err := os.MkdirTemp("", "aura-cache-audit-")
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "cache-audit: temp run dir:", err)
		return nil, exitFixture
	}
	defer func() { _ = os.RemoveAll(runDir) }()

	// Fixed deterministic skill set (Phase 11): one always:true skill (feeds the
	// messages[1] block) + one regular skill (feeds the manifest-in-Description). Both
	// are byte-stable inputs, so a turn-stable runner must produce a byte-stable
	// messages[1] + skill Description across all 20 turns.
	auditCfg, cleanupSkills, serr := setupAuditSkills(runDir)
	if serr != nil {
		_, _ = fmt.Fprintln(errOut, "cache-audit: setup fixed skill set:", serr)
		return nil, exitFixture
	}
	defer cleanupSkills()

	reg := buildRegistry()
	reg.Register(newSkillTool(auditCfg, nil)) // non-deferred; manifest rides its Description (D-06)

	client := agenttest.NewFakeClient(scriptTurns(turns)...)
	r := runner.New(runner.Deps{
		Conv:            newMemConvStore(),
		Pause:           newMemPauseStore(),
		Identity:        memIdentityStore{},
		CacheMetrics:    memCacheMetricStore{},
		ToolInvocations: memToolInvocationStore{},
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "cache-audit", ContextWindow: 1_000_000, MaxOutputTokens: 32768},
		RunDir:          runDir,
		AlwaysBlock:     alwaysBlockProvider(auditCfg), // the messages[1] always-block (D-07)
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
	if len(req.Messages) == 0 {
		return "", fmt.Errorf("request is missing messages[0]")
	}
	return prompt.PrefixHash(req.Messages, []int{0})
}

// hashMessages1 fingerprints req.Messages[1] — the messages[1] always-block (D-07).
// With a fixed always:true skill loaded this must be byte-stable across every turn
// (a skill add/remove would change it, but the fixed audit skill set never changes).
func hashMessages1(req llm.Request) (string, error) {
	if len(req.Messages) < 2 {
		return "", fmt.Errorf("request is missing messages[1] (always-block)")
	}
	return prompt.PrefixHash(req.Messages, []int{1})
}

// skillManifestName is the non-deferred skill tool whose Description carries the
// turn-stable manifest (D-06). The audit hashes its ToolDef to assert the
// manifest-in-Description never drifts turn-to-turn.
const skillManifestName = "skill"

// skillManifestHash fingerprints the skill tool's ToolDef (its manifest-in-Description,
// D-06) from req.Tools via the same canonicaljson the prefix hash uses, returning
// ok=false when the request carries no skill tool (a synthetic test request). The skill
// tool is non-deferred so its full Description is in every real request's tools array;
// with a fixed skill set it must be byte-stable across all turns.
func skillManifestHash(req llm.Request) (string, bool) {
	for i := range req.Tools {
		if req.Tools[i].Function.Name != skillManifestName {
			continue
		}
		canon, err := canonicaljson.Marshal(req.Tools[i])
		if err != nil {
			return "", false
		}
		sum := sha256.Sum256(canon)
		return hex.EncodeToString(sum[:]), true
	}
	return "", false
}

// setupAuditSkills materializes a FIXED deterministic skill set under runDir/skills:
// one always:true skill (the messages[1] always-block source) + one regular skill (the
// manifest-in-Description source). It returns a *config.Config pointing at that dir +
// the export dir, plus a cleanup. The set never changes during the replay, so a
// turn-stable runner produces a byte-stable messages[1] + skill Description.
func setupAuditSkills(runDir string) (*config.Config, func(), error) {
	skillsDir := filepath.Join(runDir, "skills")
	exportDir := filepath.Join(runDir, "skills-export")
	for _, d := range []string{skillsDir, exportDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, func() {}, err
		}
	}
	fixed := []struct {
		name, body string
		always     bool
	}{
		{"audit-haiku", "Respond only in haiku (5-7-5). A fixed always-on skill for the cache audit.", true},
		{"audit-notes", "Take structured notes. A fixed regular skill for the cache-audit manifest.", false},
	}
	for _, s := range fixed {
		dir := filepath.Join(skillsDir, s.name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, func() {}, err
		}
		fm := "---\nname: " + s.name + "\ndescription: fixed cache-audit skill\ntype: instruction\n"
		if s.always {
			fm += "always: true\n"
		}
		fm += "---\n" + s.body + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(fm), 0o600); err != nil {
			return nil, func() {}, err
		}
	}
	cfg := &config.Config{
		SkillsDir:             skillsDir,
		SkillExportDir:        exportDir,
		SkillBodyCapBytes:     32768,
		SkillManifestCapBytes: 8192,
	}
	return cfg, func() {}, nil
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
	for i, resp := range ft.Responses {
		hasText := resp.Text != ""
		hasToolCalls := len(resp.ToolCalls) > 0
		if hasText == hasToolCalls {
			return fixtureTurn{}, fmt.Errorf("response %d must have exactly one of text or tool_calls", i+1)
		}
		if hasText {
			continue
		}
		for j, tc := range resp.ToolCalls {
			if tc.ID == "" {
				return fixtureTurn{}, fmt.Errorf("response %d tool_call %d has empty id", i+1, j+1)
			}
			if tc.Name == "" {
				return fixtureTurn{}, fmt.Errorf("response %d tool_call %d has empty name", i+1, j+1)
			}
			if tc.Arguments == "" {
				return fixtureTurn{}, fmt.Errorf("response %d tool_call %d has empty arguments", i+1, j+1)
			}
			if !json.Valid([]byte(tc.Arguments)) {
				return fixtureTurn{}, fmt.Errorf("response %d tool_call %d has invalid JSON arguments", i+1, j+1)
			}
		}
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
