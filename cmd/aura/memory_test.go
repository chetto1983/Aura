package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/onboarding"
)

// recordingMemoryMCPServer is a streamable-HTTP MCP fake (modeled on
// managed_mount_test.go's httpMCPServer) that completes initialize/tools/list and,
// for tools/call, records the requested RAW tool name and returns a deterministic
// text result. It is the unit-tier stand-in for the live agent-memory sidecar — the
// real seed/read + reasoning-trace round-trip lives in 15-04's memory_integration tier.
// initializes/deletes pin Amendment #98.1's authenticated stateless transport:
// one transport initializes once for a whole submission, receives no session ID,
// and therefore emits no session DELETE.
type recordingMemoryMCPServer struct {
	*httptest.Server
	mu          sync.Mutex
	lastTool    string
	lastArgs    map[string]any
	cannedTxt   string
	initializes int
	deletes     int
	calls       []recordedCall
}

func newRecordingMemoryMCPServer(t *testing.T) *recordingMemoryMCPServer {
	t.Helper()
	rec := &recordingMemoryMCPServer{cannedTxt: "canned-memory-result"}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			rec.mu.Lock()
			rec.deletes++
			rec.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		// The SDK opens a standalone SSE stream with a bodiless GET on a
		// pre-2026-07-28 negotiated protocol (this fixture negotiates 2025-06-18)
		// unless DisableStandaloneSSE is set. This fixture is request/response
		// only, and the stream is optional per the spec, so decline it — decoding
		// a body that was never sent is what made this fixture report "decode
		// request: EOF" once memory.go's CLI path moved onto the SDK client
		// (mirrors newMCPHTTPTestServerWithTools's identical fix, mcp_status_test.go).
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "server/discover":
			// Legacy-only fixture: an error response (any non-modern error)
			// triggers the SDK's documented fallback to initialize
			// (go-sdk@v1.7.0 mcp/client.go:371-377) rather than a test failure.
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			}); err != nil {
				t.Errorf("encode discover error: %v", err)
			}
		case "initialize":
			rec.mu.Lock()
			rec.initializes++
			rec.mu.Unlock()
			writeMemoryRPC(t, w, req.ID, mcpInitializeResult("2025-06-18", "memory-test"))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMemoryRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{}})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Errorf("decode tools/call params: %v", err)
			}
			rec.mu.Lock()
			rec.lastTool = params.Name
			rec.lastArgs = params.Arguments
			rec.calls = append(rec.calls, recordedCall{tool: params.Name, args: params.Arguments})
			rec.mu.Unlock()
			writeMemoryRPC(t, w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": rec.cannedTxt}},
			})
		default:
			t.Errorf("unexpected method %q", req.Method)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *recordingMemoryMCPServer) tool() string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.lastTool
}

func (rec *recordingMemoryMCPServer) args() map[string]any {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.lastArgs
}

// sessions reports the MCP handshakes and session teardowns observed at the wire.
func (rec *recordingMemoryMCPServer) sessions() (initializes, deletes int) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.initializes, rec.deletes
}

func (rec *recordingMemoryMCPServer) recordedCalls() []recordedCall {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]recordedCall(nil), rec.calls...)
}

// writeMemoryRPC runs on httptest handler goroutines — t.Fatalf is illegal off the
// test goroutine (it would leak the handler), so failures report via t.Errorf (WR-05).
func writeMemoryRPC(t *testing.T, w http.ResponseWriter, id *int64, result any) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Errorf("marshal result: %v", err)
		http.Error(w, "marshal", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  json.RawMessage(raw),
	}); err != nil {
		t.Errorf("encode rpc: %v", err)
	}
}

// withMemoryServerAt writes a managed config that points the `memory` managed server
// at url as a streamable_http server, so effectiveManagedMCPServer("memory") resolves
// to the fake transport (RemoteHTTP trust is inferred for a streamable_http server, so
// it is runnable, not blocked).
func withMemoryServerAt(t *testing.T, url string) {
	t.Helper()
	t.Setenv(
		"AURA_AGENT_MEMORY_MCP_AUTH_SECRET",
		"memory-command-test-secret-that-is-at-least-32-bytes",
	)
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		memoryServerName: {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    url,
			Source: "recipe:memory",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}
}

// scopedCtx carries an identity the way runMemory's withOperatorIdentity does in
// production. Without it every call fails on scoping, which would make the negative cases
// below pass for the wrong reason.
func scopedCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), "identity-under-test")
}

func TestMemoryVerbMapping(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	cases := []struct {
		name     string
		args     []string
		wantTool string
		argKey   string
		argVal   string
	}{
		{"search", []string{"search", "mario rossi"}, "memory_search", "query", "mario rossi"},
		{"search-as-of", []string{"search", "mario", "--as-of", "2026-01-01T00:00:00Z"}, "memory_search", "as_of", "2026-01-01T00:00:00Z"},
		{"facts", []string{"facts", "Mario Rossi"}, "memory_facts_about", "entity", "Mario Rossi"},
		{"facts-predicate", []string{"facts", "Mario Rossi", "lives_in"}, "memory_facts_about", "predicate", "lives_in"},
		{"entities", []string{"entities"}, "memory_entities", "", ""},
		{"digest", []string{"digest"}, "memory_digest", "", ""},
		{"remember", []string{"remember", "aura", "validates", "memory wiring", "Aura validates its memory wiring."}, "memory_upsert_fact", "statement", "Aura validates its memory wiring."},
		{"merge", []string{"merge", "M. Rossi", "Mario Rossi"}, "memory_merge_entities", "target", "Mario Rossi"},
		{"forget-entity", []string{"forget", "--entity", "Mario Rossi"}, "memory_forget", "entity", "Mario Rossi"},
		{"forget-run", []string{"forget", "--run", "run-7"}, "memory_forget", "", ""},
		{"schema", []string{"schema"}, "graph_schema", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := runMemoryCommand(scopedCtx(), tc.args, &buf); err != nil {
				t.Fatalf("runMemoryCommand(%v): %v", tc.args, err)
			}
			if got := rec.tool(); got != tc.wantTool {
				t.Fatalf("verb %q dispatched tool %q, want RAW %q", tc.name, got, tc.wantTool)
			}
			if strings.HasPrefix(rec.tool(), "memory__") {
				t.Fatalf("verb %q dispatched a namespaced tool %q; expected RAW wire name", tc.name, rec.tool())
			}
			if !strings.Contains(buf.String(), rec.cannedTxt) {
				t.Fatalf("verb %q output %q missing canned result %q", tc.name, buf.String(), rec.cannedTxt)
			}
			if tc.argKey != "" {
				if got, _ := rec.args()[tc.argKey].(string); got != tc.argVal {
					t.Fatalf("verb %q arg %q = %q, want %q", tc.name, tc.argKey, got, tc.argVal)
				}
			}
		})
	}
}

// A relationship is just a fact now: subject, predicate, object. memory_create_relationship
// was its own tool on the previous sidecar and has no counterpart here, which is the
// point — one write verb instead of four that differed only in which node type they minted.
func TestMemoryRememberCarriesTheSupersedesFlag(t *testing.T) {
	t.Parallel()

	// Without the flag, an upsert ADDS. Both versions then stay valid at the same
	// instant and retrieval can return the stale one — the one trap the bitemporal
	// model introduces in exchange for the one it removes.
	tool, args, err := memoryRememberArgs([]string{"Mario", "KNOWS", "Luigi", "Mario knows Luigi."})
	if err != nil {
		t.Fatal(err)
	}
	if tool != "memory_upsert_fact" {
		t.Fatalf("tool = %q, want memory_upsert_fact", tool)
	}
	if args["supersedes"] != false {
		t.Fatalf("supersedes = %v, want false by default", args["supersedes"])
	}
	if args["statement"] != "Mario knows Luigi." {
		t.Fatalf("statement = %q", args["statement"])
	}

	_, args, err = memoryRememberArgs([]string{"--supersedes", "Mario", "lives_in", "Bologna", "Mario lives in Bologna."})
	if err != nil {
		t.Fatal(err)
	}
	if args["supersedes"] != true {
		t.Fatal("--supersedes did not reach the call")
	}
}

func TestMemoryRememberCarriesEntityKinds(t *testing.T) {
	t.Parallel()
	_, args, err := memoryRememberArgs([]string{
		"--subject-kind", "Person", "--object-kind", "Location",
		"Mario", "lives_in", "Torino", "Mario lives in Torino.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if args["subject_kind"] != "Person" || args["object_kind"] != "Location" {
		t.Fatalf("entity kinds = %v/%v", args["subject_kind"], args["object_kind"])
	}
	if args["subject"] != "Mario" || args["statement"] != "Mario lives in Torino." {
		t.Fatalf("kind flags leaked into positional args: %#v", args)
	}
}

// TestMemoryRememberAlwaysCarriesASourceRun guards the provenance needed to
// find and remove a run's writes.
func TestMemoryRememberAlwaysCarriesASourceRun(t *testing.T) {
	t.Parallel()

	_, args, err := memoryRememberArgs([]string{"Mario", "KNOWS", "Luigi", "Mario knows Luigi."})
	if err != nil {
		t.Fatal(err)
	}
	run := sourceRun(args)
	if run == "" {
		t.Fatal("remember sent no source run; the tool rejects the call")
	}
	// Grouped by DAY so `aura memory forget --run cli-2026-08-02` can reverse a session
	// of manual entry. Per-invocation would be precise and impossible to type back.
	if want := cliRunID(time.Now().UTC()); run != want {
		t.Errorf("default run = %q, want %q", run, want)
	}

	_, args, err = memoryRememberArgs(
		[]string{"--run", "import-2026", "Mario", "KNOWS", "Luigi", "Mario knows Luigi."})
	if err != nil {
		t.Fatal(err)
	}
	if sourceRun(args) != "import-2026" {
		t.Errorf("--run = %v, want it to override the daily default", args["source"])
	}
	if args["subject"] != "Mario" {
		t.Errorf("--run leaked into the positional args: subject = %v", args["subject"])
	}
}

func TestMemoryForgetRunUsesStructuredSource(t *testing.T) {
	t.Parallel()
	_, args, err := memoryForgetArgs([]string{"--run", "run-7"})
	if err != nil {
		t.Fatal(err)
	}
	if run := sourceRun(args); run != "run-7" {
		t.Fatalf("source run = %q, want run-7", run)
	}
}

func sourceRun(args map[string]any) string {
	source, _ := args["source"].(map[string]any)
	run, _ := source["run_id"].(string)
	return run
}

// Forgetting is the only irreversible act on this surface, so it must default to a
// dry run and require an explicit --apply.
func TestMemoryForgetIsADryRunUntilApplied(t *testing.T) {
	t.Parallel()

	_, args, err := memoryForgetArgs([]string{"--entity", "Mario Rossi"})
	if err != nil {
		t.Fatal(err)
	}
	if args["dry_run"] != true {
		t.Fatal("forget without --apply is not a dry run")
	}
	_, args, err = memoryForgetArgs([]string{"--entity", "Mario Rossi", "--apply"})
	if err != nil {
		t.Fatal(err)
	}
	if args["dry_run"] != false {
		t.Fatal("--apply did not clear dry_run")
	}
	// Matching nothing would sweep everything; it must be refused.
	if _, _, err := memoryForgetArgs([]string{"--apply"}); err == nil {
		t.Fatal("forget with no matcher was accepted")
	}
}

func TestMemoryVerbMappingNegativeCases(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	cases := []struct {
		name string
		args []string
	}{
		{"no-args", nil},
		{"unknown-verb", []string{"frobnicate"}},
		{"search-missing-query", []string{"search"}},
		{"facts-missing-entity", []string{"facts"}},
		{"remember-too-few", []string{"remember", "subject", "predicate", "object"}},
		{"merge-too-few", []string{"merge", "duplicate"}},
		// A forget that matches nothing would sweep everything, so it must be refused
		// rather than sent.
		{"forget-no-matcher", []string{"forget"}},
		{"forget-apply-no-matcher", []string{"forget", "--apply"}},
		// Verbs of the previous sidecar. They must fail as UNKNOWN rather than reach
		// a server that does not implement them.
		{"gone-context", []string{"context", "anything"}},
		{"gone-sessions", []string{"sessions"}},
		{"gone-conversation", []string{"conversation", "sess-1"}},
		{"gone-store-message", []string{"store-message", "sess-1", "user", "hi"}},
		{"gone-add-entity", []string{"add-entity", "Mario"}},
		{"gone-add-fact", []string{"add-fact", "a", "b", "c"}},
		{"gone-add-preference", []string{"add-preference", "ui", "dark"}},
		{"gone-get-entity", []string{"get-entity", "Mario"}},
		{"gone-relationship", []string{"relationship", "a", "KNOWS", "b"}},
		{"gone-update", []string{"update", "fact", "f-1", "object=x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := runMemoryCommand(scopedCtx(), tc.args, &buf); err == nil {
				t.Fatalf("args %v: expected non-nil error, got nil (output %q)", tc.args, buf.String())
			}
		})
	}
}

func TestMemoryNotConfigured(t *testing.T) {
	// Memory is default-on (15-02 inject-unless-disabled), so an empty managed
	// config still resolves the catalog recipe. The unresolvable state is an
	// explicit `aura mcp disable memory` (Enabled=false, D-09).
	path := withTempMCPConfig(t)
	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		memoryServerName: {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://127.0.0.1:1/mcp/",
			Source:  "recipe:memory",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			Enabled: &disabled,
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}
	var buf bytes.Buffer
	err := runMemoryCommand(scopedCtx(), []string{"search", "x"}, &buf)
	if err == nil {
		t.Fatalf("expected error when memory server is disabled")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want a not-configured message", err)
	}
}

// TestScopeMemoryArgs pins the scoping guard on the CLI memory path. The memory server
// treats a missing user_identifier as "no scope", so an unstamped call wrote a
// :Conversation with a NULL owner and zero HAS_CONVERSATION edges — data owned by nobody
// and invisible to every scoped read meant to return it, with anything extracted from it
// landing in the "global" deduplication scope where it can never merge with the
// owner-scoped entities the agent records.
func TestScopeMemoryArgs(t *testing.T) {
	t.Run("refuses to invent an identity when the context carries none", func(t *testing.T) {
		// This used to fall back to identityctx.LocalOperatorIdentity, which reads as
		// fail-closed and is not: first login retires that seed, so the CLI addressed a
		// deleted tenant while the cockpit used the enrolled one. `aura memory facts
		// Davide` answered 0 with three facts in the graph. Guessing an owner is the
		// failure, not the safety net.
		_, err := scopeMemoryArgs(context.Background(), map[string]any{"session_id": "s1"})
		if err == nil {
			t.Fatal("expected an error rather than a guessed owner")
		}
	})

	t.Run("uses the identity on the context", func(t *testing.T) {
		ctx := identityctx.WithIdentityID(context.Background(), "identity-1")
		got, err := scopeMemoryArgs(ctx, map[string]any{"session_id": "s1"})
		if err != nil {
			t.Fatalf("scopeMemoryArgs: %v", err)
		}
		if got["user_identifier"] != "identity-1" {
			t.Fatalf("user_identifier = %v, want identity-1", got["user_identifier"])
		}
		if got["session_id"] != "s1" {
			t.Fatalf("existing args must survive: %#v", got)
		}
	})

	t.Run("never silently rescopes an explicit user_identifier", func(t *testing.T) {
		ctx := identityctx.WithIdentityID(context.Background(), "identity-1")
		got, err := scopeMemoryArgs(ctx, map[string]any{"user_identifier": "someone-else"})
		if err != nil {
			t.Fatalf("scopeMemoryArgs: %v", err)
		}
		if got["user_identifier"] != "someone-else" {
			t.Fatalf("explicit scope was overwritten: %v", got["user_identifier"])
		}
	})
}

// TestStoreConfirmedOpensOneMCPSessionForTheWholeSubmission asserts the wire boundary:
// ONE transport and ONE handshake, however many facts the seed maps to. That property is
// the whole reason writeProfile takes a session instead of a call — nine facts over nine
// handshakes would pay the initialize nine times.
func TestStoreConfirmedOpensOneMCPSessionForTheWholeSubmission(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	store := newMemoryProfileStore()
	if err := store.StoreConfirmed(context.Background(), "id-uuid", fullSeedAnswers); err != nil {
		t.Fatalf("StoreConfirmed: %v", err)
	}

	initializes, deletes := rec.sessions()
	if initializes != 1 {
		t.Errorf("MCP handshakes = %d, want exactly 1 for the whole submission", initializes)
	}
	if deletes != 0 {
		t.Errorf("MCP session teardowns = %d, want 0 in authenticated stateless mode", deletes)
	}
	calls := rec.recordedCalls()
	if len(calls) != 9 {
		t.Fatalf("tools/call requests = %d, want 8 profile facts + the sentinel", len(calls))
	}
	for i, call := range calls {
		if call.tool != "memory_upsert_fact" {
			t.Errorf("wire call %d tool = %q, want memory_upsert_fact", i, call.tool)
		}
		if call.args["user_identifier"] != "id-uuid" {
			t.Errorf("wire call %d missing user_identifier scope: %#v", i, call.args)
		}
	}
	if last := calls[len(calls)-1]; last.args["predicate"] != onboarding.PredicateOnboardingCompleted {
		t.Errorf("last wire call = %#v, want the completion sentinel", last.args)
	}
}

// TestStoreSkippedOpensOneMCPSession pins the stateless cheap-skip path at the
// same wire level: one initialize, one tools/call, no teardown.
func TestStoreSkippedOpensOneMCPSession(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	if err := newMemoryProfileStore().StoreSkipped(context.Background(), "id-uuid"); err != nil {
		t.Fatalf("StoreSkipped: %v", err)
	}
	initializes, deletes := rec.sessions()
	if initializes != 1 || deletes != 0 {
		t.Errorf("skip transport = %d initialize / %d delete, want 1/0", initializes, deletes)
	}
	if got := len(rec.recordedCalls()); got != 1 {
		t.Errorf("skip tools/call requests = %d, want 1", got)
	}
}
