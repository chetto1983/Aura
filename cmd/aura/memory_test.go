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

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/onboarding"
)

// recordingMemoryMCPServer is a streamable-HTTP MCP fake (modeled on
// managed_mount_test.go's httpMCPServer) that completes initialize/tools/list and,
// for tools/call, records the requested RAW tool name and returns a deterministic
// text result. It is the unit-tier stand-in for the live agent-memory sidecar — the
// real seed/read + reasoning-trace round-trip lives in 15-04's memory_integration tier.
// initializes/deletes count MCP SESSIONS at the wire, not calls made through a Go seam:
// a streamable-HTTP session is opened by exactly one `initialize` and closed by exactly
// one `DELETE`, so those two counters are the direct evidence for Amendment #95's
// "the whole submission opens ONE MCP session, asserted on the transport".
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
		case "initialize":
			rec.mu.Lock()
			rec.initializes++
			rec.mu.Unlock()
			w.Header().Set("Mcp-Session-Id", "sess-memory")
			writeMemoryRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
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
		{"context", []string{"context", "what does the user prefer"}, "memory_get_context", "query", "what does the user prefer"},
		{"sessions", []string{"sessions"}, "memory_list_sessions", "", ""},
		{"conversation", []string{"conversation", "sess-1"}, "memory_get_conversation", "session_id", "sess-1"},
		{"add-entity", []string{"add-entity", "Mario Rossi", "PERSON", "a colleague"}, "memory_add_entity", "name", "Mario Rossi"},
		{"add-fact", []string{"add-fact", "aura", "validates", "memory wiring"}, "memory_add_fact", "subject", "aura"},
		{"add-preference", []string{"add-preference", "ui", "dark mode"}, "memory_add_preference", "category", "ui"},
		{"store-message", []string{"store-message", "sess-1", "user", "hello world"}, "memory_store_message", "session_id", "sess-1"},
		{"get-entity", []string{"get-entity", "Mario Rossi"}, "memory_get_entity", "name", "Mario Rossi"},
		{"facts-by-subject", []string{"facts", "Mario Rossi"}, "memory_get_facts", "subject", "Mario Rossi"},
		{"facts-semantic", []string{"facts", "--like", "dove abita"}, "memory_get_facts", "query", "dove abita"},
		{"relationship", []string{"relationship", "Mario", "KNOWS", "Luigi"}, "memory_create_relationship", "from_entity", "Mario"},
		{"update-entity", []string{"update", "entity", "ent-1", "name=Davide"}, "memory_update", "name", "Davide"},
		{"update-fact", []string{"update", "fact", "f-1", "object_value=Bologna"}, "memory_update", "node_id", "f-1"},
		{"forget-preference", []string{"forget", "preference", "pref-123"}, "memory_forget", "node_id", "pref-123"},
		{"forget-fact", []string{"forget", "fact", "fact-456"}, "memory_forget", "node_type", "fact"},
		{"forget-entity", []string{"forget", "entity", "ent-789"}, "memory_forget", "node_type", "entity"},
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
		{"add-fact-too-few", []string{"add-fact", "subject", "predicate"}},
		{"forget-too-few", []string{"forget", "preference"}},
		{"facts-missing-subject", []string{"facts"}},
		{"facts-like-missing-text", []string{"facts", "--like"}},
		{"about-without-value", []string{"add-preference", "home", "lives in", "--about"}},
		{"about-empty-value", []string{"add-preference", "home", "lives in", "--about", " , "}},
		{"update-too-few", []string{"update", "entity", "ent-1"}},
		{"update-unknown-node-type", []string{"update", "widget", "w-1", "name=x"}},
		{"update-field-not-on-that-type", []string{"update", "fact", "f-1", "name=x"}},
		{"update-not-an-assignment", []string{"update", "entity", "e-1", "name"}},
		{"update-unparsable-number", []string{"update", "fact", "f-1", "confidence=high"}},
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

// TestMemoryAppliesToReachesTheWire pins the one flag this positional CLI accepts.
// Without applies_to a preference hangs off the user node with nothing connecting it to
// what it is ABOUT, so recall can only find it by wording — the structural half of the
// graph is unreachable from the operator path.
func TestMemoryAppliesToReachesTheWire(t *testing.T) {
	tool, args, err := memoryVerbToTool("add-preference",
		[]string{"home", "vive", "a", "Caraglio", "--about", "Caraglio, Davide"})
	if err != nil {
		t.Fatalf("add-preference with --about: %v", err)
	}
	if tool != "memory_add_preference" {
		t.Fatalf("tool = %q, want memory_add_preference", tool)
	}
	if got := args["preference"]; got != "vive a Caraglio" {
		t.Fatalf("preference = %q; the flag must not leak into the text", got)
	}
	if got := args["category"]; got != "home" {
		t.Fatalf("category = %q, want home", got)
	}
	names, ok := args["applies_to"].([]string)
	if !ok || len(names) != 2 || names[0] != "Caraglio" || names[1] != "Davide" {
		t.Fatalf("applies_to = %#v, want [Caraglio Davide] with surrounding spaces trimmed", args["applies_to"])
	}

	_, bare, err := memoryVerbToTool("add-preference", []string{"ui", "dark mode"})
	if err != nil {
		t.Fatalf("add-preference without --about: %v", err)
	}
	if _, present := bare["applies_to"]; present {
		t.Fatalf("applies_to must be absent when not given, got %#v", bare["applies_to"])
	}
}

// TestMemoryUpdateFieldTypes pins that update sends each field in the type the tool
// expects. A confidence delivered as the string "0.4" is not a number to the server, and
// aliases is a list — passing either as raw text is accepted by the wire and then ignored.
func TestMemoryUpdateFieldTypes(t *testing.T) {
	_, args, err := memoryVerbToTool("update",
		[]string{"entity", "ent-1", "name=Davide", "aliases=David, Dave"})
	if err != nil {
		t.Fatalf("update entity: %v", err)
	}
	if args["name"] != "Davide" || args["node_id"] != "ent-1" || args["node_type"] != "entity" {
		t.Fatalf("update entity args = %#v", args)
	}
	aliases, ok := args["aliases"].([]string)
	if !ok || len(aliases) != 2 || aliases[1] != "Dave" {
		t.Fatalf("aliases = %#v, want a trimmed two-element list", args["aliases"])
	}

	_, prefArgs, err := memoryVerbToTool("update", []string{"preference", "p-1", "confidence=0.4"})
	if err != nil {
		t.Fatalf("update preference: %v", err)
	}
	if got, ok := prefArgs["confidence"].(float64); !ok || got != 0.4 {
		t.Fatalf("confidence = %#v, want float64(0.4)", prefArgs["confidence"])
	}
}

// TestMemoryUpdateRejectsFieldsTheServerWouldDrop is the reason the whitelist exists:
// memory_update uses only the fields belonging to the node type and ignores the rest, so
// an unchecked typo would return a success the operator has no way to distinguish from a
// real correction.
func TestMemoryUpdateRejectsFieldsTheServerWouldDrop(t *testing.T) {
	_, _, err := memoryVerbToTool("update", []string{"fact", "f-1", "category=home"})
	if err == nil {
		t.Fatal("expected an error: a fact has no category field")
	}
	if !strings.Contains(err.Error(), "object_value") {
		t.Fatalf("the error should list what a fact does accept, got: %v", err)
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

// TestStoreConfirmedOpensOneMCPSessionForTheWholeSubmission is Amendment #95's acceptance
// gate, asserted ON THE TRANSPORT rather than inferred: the store drives the REAL
// openMemoryMCP against the streamable-HTTP fake, and the fake counts protocol events. A
// full seed maps to nine tools/call requests; those must ride ONE `initialize` and end in
// ONE session `DELETE`. Before this change each write opened, handshook, called and closed
// its own session — nine handshakes, the "1000 chiamate" the directive names.
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
	if deletes != 1 {
		t.Errorf("MCP session teardowns = %d, want exactly 1", deletes)
	}
	calls := rec.recordedCalls()
	if len(calls) != 9 {
		t.Fatalf("tools/call requests = %d, want 9 over that single session", len(calls))
	}
	for _, c := range calls {
		if c.args["user_identifier"] != "id-uuid" {
			t.Errorf("wire call %q missing user_identifier scope: %#v", c.tool, c.args)
		}
		if c.tool == "memory_store_message" {
			t.Error("onboarding wrote a :Message node at the wire (Amendment #95 deletes the raw-draft net)")
		}
	}
	if calls[len(calls)-1].args["predicate"] != onboarding.PredicateOnboardingCompleted {
		t.Errorf("last wire call = %#v, want the completion sentinel", calls[len(calls)-1].args)
	}
}

// TestStoreSkippedOpensOneMCPSession pins the cheap-skip path at the same wire level: one
// handshake, one tools/call, one teardown.
func TestStoreSkippedOpensOneMCPSession(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	if err := newMemoryProfileStore().StoreSkipped(context.Background(), "id-uuid"); err != nil {
		t.Fatalf("StoreSkipped: %v", err)
	}
	initializes, deletes := rec.sessions()
	if initializes != 1 || deletes != 1 {
		t.Errorf("skip sessions = %d initialize / %d delete, want 1/1", initializes, deletes)
	}
	if got := len(rec.recordedCalls()); got != 1 {
		t.Errorf("skip tools/call requests = %d, want 1", got)
	}
}
