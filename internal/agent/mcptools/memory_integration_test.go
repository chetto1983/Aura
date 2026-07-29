//go:build memory_integration

// Live memory-MCP mount tier (port of spike 032). Proves Aura's bridge mounts the
// MODEL-FACING slice of the live, rebuilt sidecar: every mounted spec is non-deferred
// and namespaced memory__*, the six model-facing verbs mount, and the tools hidden by
// cb02109 (hide-from-model, never-remove-from-server) stay OFF the model's registry
// while remaining served for Aura's own direct CallTool consumers. hiddenFromModel is
// the single source of truth for the split — this tier asserts the live wiring honours
// it, it does not re-declare the list.
//
// No-skip-as-green (CLAUDE.md): when AURA_AGENT_MEMORY_MCP_URL is unset under $CI the
// test t.Fatals (a skipped tier fails the gate, never passes it); locally it t.Skips.
package mcptools

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func memoryIntegrationToken(t *testing.T, subject string) string {
	t.Helper()
	secret := os.Getenv("AURA_AGENT_MEMORY_MCP_AUTH_SECRET")
	if len(secret) < 32 {
		t.Fatal("AURA_AGENT_MEMORY_MCP_AUTH_SECRET must be at least 32 bytes")
	}
	now := time.Now().UTC().Unix()
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal JWT header: %v", err)
	}
	claims, err := json.Marshal(map[string]any{
		"sub": subject, "iss": "aura", "aud": "agent-memory",
		"scope": "memory:access", "iat": now, "exp": now + 60,
	})
	if err != nil {
		t.Fatalf("marshal JWT claims: %v", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// reapIdleHTTPConns drains the shared http.DefaultClient's idle keep-alive
// connections at test end. The live streamable-HTTP MCP transport (mcp.OpenServer)
// uses http.DefaultClient, whose parked readLoop/writeLoop goroutines otherwise trip
// the package goleak TestMain even though Close() ended the MCP session. Reaping idle
// connections returns those goroutines synchronously; it is test-only and never
// touches production Close() semantics (which must not close a shared client).
func reapIdleHTTPConns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		// Give the netpoller a beat to unpark the reaped readLoop/writeLoop.
		time.Sleep(200 * time.Millisecond)
	})
}

func TestMemoryLiveScopedGraphToolsAcceptUserIdentifier(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cli, err := mcp.OpenServer(ctx, "memory-scoped-graph-probe", liveMemoryServer(endpoint))
	if err != nil {
		t.Fatalf("open streamable-http memory MCP at %s: %v", endpoint, err)
	}
	defer func() { _ = cli.Close() }()

	uid := fmt.Sprintf("codex-live-%d", time.Now().UnixNano())
	subject := "Codex Live Subject " + uid
	entityName := "Codex Live Place " + uid

	entityText, err := cli.CallTool(ctx, "memory_add_entity", map[string]any{
		"name":            entityName,
		"entity_type":     "LOCATION",
		"user_identifier": uid,
	})
	if err != nil {
		t.Fatalf("memory_add_entity: %v", err)
	}
	if strings.Contains(entityText, "disabled for user-scoped sessions") || !strings.Contains(entityText, `"stored": true`) {
		t.Fatalf("memory_add_entity returned unexpected result: %s", entityText)
	}

	factText, err := cli.CallTool(ctx, "memory_add_fact", map[string]any{
		"subject":         subject,
		"predicate":       "has_marker",
		"object_value":    "ok",
		"user_identifier": uid,
	})
	if err != nil {
		t.Fatalf("memory_add_fact: %v", err)
	}
	if strings.Contains(factText, "disabled for user-scoped sessions") || !strings.Contains(factText, `"stored": true`) {
		t.Fatalf("memory_add_fact returned unexpected result: %s", factText)
	}

	factsText, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{
		"subject":         subject,
		"user_identifier": uid,
	})
	if err != nil {
		t.Fatalf("memory_get_facts: %v", err)
	}
	if strings.Contains(factsText, "disabled for user-scoped sessions") || !strings.Contains(factsText, `"fact_count": 1`) {
		t.Fatalf("memory_get_facts did not return the scoped fact: %s", factsText)
	}

	otherText, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{
		"subject":         subject,
		"user_identifier": uid + "-other",
	})
	if err != nil {
		t.Fatalf("memory_get_facts other user: %v", err)
	}
	if !strings.Contains(otherText, `"fact_count": 0`) {
		t.Fatalf("memory_get_facts leaked scoped fact to another user: %s", otherText)
	}
}

func TestMemoryLiveRejectsUnauthenticatedAndPayloadForgedIdentity(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if unauthenticated, err := mcp.OpenHTTP(ctx, "memory-unauthenticated", mcp.HTTPConfig{
		URL: endpoint,
	}); err == nil {
		_ = unauthenticated.Close()
		t.Fatal("unauthenticated Agent Memory initialize succeeded")
	} else if !strings.Contains(err.Error(), "unauthorized (401)") {
		t.Fatalf("unauthenticated initialize error = %v, want 401", err)
	}

	alice := fmt.Sprintf("auth-alice-%d", time.Now().UnixNano())
	bob := alice + "-bob"
	subject := "Forged Scope Probe " + alice
	aliceClient, err := mcp.OpenHTTP(ctx, "memory-auth-alice", mcp.HTTPConfig{
		URL: endpoint, BearerToken: memoryIntegrationToken(t, alice),
	})
	if err != nil {
		t.Fatalf("open Alice static-token client: %v", err)
	}
	defer func() { _ = aliceClient.Close() }()

	addedText, err := aliceClient.CallTool(ctx, "memory_add_fact", map[string]any{
		"subject": subject, "predicate": "has_marker", "object_value": "alice-only",
		"user_identifier": bob,
	})
	if err != nil {
		t.Fatalf("forged-body add_fact: %v", err)
	}
	var added struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(addedText), &added); err != nil || added.ID == "" {
		t.Fatalf("decode added fact %q: id=%q err=%v", addedText, added.ID, err)
	}
	defer func() {
		_, _ = aliceClient.CallTool(context.Background(), "memory_forget", map[string]any{
			"node_type": "fact", "node_id": added.ID, "user_identifier": bob,
		})
	}()

	aliceRead, err := aliceClient.CallTool(ctx, "memory_get_facts", map[string]any{
		"subject": subject, "user_identifier": bob,
	})
	if err != nil || !strings.Contains(aliceRead, `"fact_count": 1`) {
		t.Fatalf("Alice token did not read Alice-owned forged-body write: err=%v body=%s", err, aliceRead)
	}

	bobClient, err := mcp.OpenHTTP(ctx, "memory-auth-bob", mcp.HTTPConfig{
		URL: endpoint, BearerToken: memoryIntegrationToken(t, bob),
	})
	if err != nil {
		t.Fatalf("open Bob static-token client: %v", err)
	}
	defer func() { _ = bobClient.Close() }()
	bobRead, err := bobClient.CallTool(ctx, "memory_get_facts", map[string]any{
		"subject": subject, "user_identifier": alice,
	})
	if err != nil {
		t.Fatalf("Bob forged-body read: %v", err)
	}
	if !strings.Contains(bobRead, `"fact_count": 0`) || strings.Contains(bobRead, "alice-only") {
		t.Fatalf("Bob token crossed tenant boundary: %s", bobRead)
	}
}

func TestMemoryLiveSameSessionIDIsTenantIsolated(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := mcp.OpenServer(ctx, "memory-short-term-isolation", liveMemoryServer(endpoint))
	if err != nil {
		t.Fatalf("open memory MCP: %v", err)
	}
	defer func() { _ = client.Close() }()

	run := fmt.Sprintf("%d", time.Now().UnixNano())
	sessionID := "shared-session-" + run
	alice := "short-alice-" + run
	bob := "short-bob-" + run
	aliceMarker := "ALICE-ONLY-" + run
	bobMarker := "BOB-ONLY-" + run
	for _, write := range []struct {
		owner  string
		marker string
	}{
		{owner: alice, marker: aliceMarker},
		{owner: bob, marker: bobMarker},
	} {
		text, callErr := client.CallTool(ctx, "memory_store_message", map[string]any{
			"session_id": sessionID, "role": "user", "content": write.marker,
			"user_identifier": write.owner,
		})
		if callErr != nil || !strings.Contains(text, `"stored": true`) {
			t.Fatalf("store %s: err=%v body=%s", write.owner, callErr, text)
		}
	}

	aliceConversation, err := client.CallTool(ctx, "memory_get_conversation", map[string]any{
		"session_id": sessionID, "user_identifier": alice,
	})
	if err != nil {
		t.Fatalf("get Alice conversation: %v", err)
	}
	bobConversation, err := client.CallTool(ctx, "memory_get_conversation", map[string]any{
		"session_id": sessionID, "user_identifier": bob,
	})
	if err != nil {
		t.Fatalf("get Bob conversation: %v", err)
	}
	if !strings.Contains(aliceConversation, aliceMarker) || strings.Contains(aliceConversation, bobMarker) {
		t.Fatalf("Alice conversation crossed tenant boundary: %s", aliceConversation)
	}
	if !strings.Contains(bobConversation, bobMarker) || strings.Contains(bobConversation, aliceMarker) {
		t.Fatalf("Bob conversation crossed tenant boundary: %s", bobConversation)
	}
}

// TestMemoryLiveNoPrincipalBridgeScopesToOperator is the end-to-end guard for the
// fail-open fix: it seeds a fact owned by a FOREIGN user directly on the live server,
// then drives a memory read THROUGH THE REAL BRIDGE with NO principal in context. The
// bridge must fall back to the local operator identity so the read is operator-scoped
// and the foreign fact never surfaces. Before the fix the bridge forwarded the call
// bare, the server ran its unscoped global query, and this same read returned the
// foreign fact — the leak this test now pins closed.
//
// The read goes through memory__memory_search's 'facts' bucket: cb02109 hid get_facts
// from the model (its exact-subject lookup is now the first leg of the facts bucket,
// eed51614), so search is the model-facing tool that exercises the same scoped read.
func TestMemoryLiveNoPrincipalBridgeScopesToOperator(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	server := liveMemoryServer(endpoint)

	// Seed a fact under a foreign user (NOT the operator), directly via the live MCP.
	foreign := fmt.Sprintf("foreign-%d", time.Now().UnixNano())
	subject := "NoPrincipal Leak Probe " + foreign
	seed, err := mcp.OpenServer(ctx, "seed-foreign-fact", server)
	if err != nil {
		t.Fatalf("open seed MCP at %s: %v", endpoint, err)
	}
	if _, err := seed.CallTool(ctx, "memory_add_fact", map[string]any{
		"subject": subject, "predicate": "has_marker", "object_value": "secret",
		"user_identifier": foreign,
	}); err != nil {
		_ = seed.Close()
		t.Fatalf("seed foreign fact: %v", err)
	}
	_ = seed.Close()

	// Mount through the real bridge and read facts with NO identity on the context —
	// the CLI / no-principal path that the fix protects.
	reg := tools.NewRegistry()
	closer, _, err := MountManagedServer(ctx, ctx, reg, "memory", server)
	if err != nil {
		t.Fatalf("MountManagedServer: %v", err)
	}
	defer func() { _ = closer() }()
	tl, ok := reg.Get("memory__memory_search")
	if !ok {
		t.Fatal("memory__memory_search not mounted")
	}
	execCtx := tools.WithToolCallContext(ctx, "sess", "tc1", t.TempDir(), 8192)

	// The facts bucket does the exact-subject lookup, user-scoped; querying the foreign
	// subject with no principal must resolve to the operator and return nothing.
	res, err := tl.Execute(execCtx, json.RawMessage(fmt.Sprintf(`{"query":%q,"memory_types":["facts"]}`, subject)))
	if err != nil {
		t.Fatalf("bridge Execute memory_search: %v", err)
	}
	// The search must actually have run and returned the facts bucket (not an error
	// swallowed into a vacuous pass); then, operator-scoped, the foreign fact must NOT
	// surface. The leak signal is the seeded fact's OWN marker fields — its predicate
	// "has_marker" and object "secret" — NOT the subject: memory_search echoes the query
	// verbatim in a top-level "query" field, so the subject string is always present in the
	// result and is not a leak signal (asserting it was a false positive — the facts bucket
	// came back [] and the read was correctly scoped, yet the subject matched the echo).
	if !strings.Contains(res.Preview, `"facts"`) {
		t.Fatalf("memory_search did not return the facts bucket — read path is untested: %s", res.Preview)
	}
	if strings.Contains(res.Preview, "has_marker") || strings.Contains(res.Preview, `"secret"`) {
		t.Fatalf("no-principal bridge read leaked a foreign user's fact (fail-open NOT closed): %s", res.Preview)
	}

	// Cross-check the seed really exists under the foreign user (the read is scoped, not
	// empty-by-accident). get_facts stays served for direct callers even though it is hidden
	// from the model, so this verification path is unaffected by cb02109.
	verify, err := mcp.OpenServer(ctx, "verify-foreign-fact", server)
	if err != nil {
		t.Fatalf("open verify MCP: %v", err)
	}
	defer func() { _ = verify.Close() }()
	vtext, err := verify.CallTool(ctx, "memory_get_facts", map[string]any{"subject": subject, "user_identifier": foreign})
	if err != nil {
		t.Fatalf("verify foreign fact: %v", err)
	}
	if !strings.Contains(vtext, `"fact_count": 1`) {
		t.Fatalf("seed fact not retrievable under its owner — test would be vacuous: %s", vtext)
	}
}

// memoryEndpointOrGate resolves the live sidecar URL from AURA_AGENT_MEMORY_MCP_URL
// (or AURA_AGENT_MEMORY_MCP_PORT). Empty under $CI is a HARD failure (no-skip-as-green);
// empty locally is a skip.
func memoryEndpointOrGate(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL")); v != "" {
		return v
	}
	if port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT")); port != "" {
		return "http://127.0.0.1:" + port + "/mcp/"
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_AGENT_MEMORY_MCP_URL (or _PORT) must be set under CI — a skipped memory_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
	}
	t.Skip("set AURA_AGENT_MEMORY_MCP_URL (or AURA_AGENT_MEMORY_MCP_PORT) + bring the stack up to run the memory_integration tier")
	return ""
}

func liveMemoryServer(endpoint string) mcp.ManagedServer {
	return mcp.ManagedServer{
		Type:   mcp.ServerTypeStreamableHTTP,
		URL:    endpoint,
		Source: mcp.SourceRecipeMemory,
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	}
}

// TestMemoryLiveMount mounts the running agent-memory sidecar through Aura's managed
// bridge and asserts the model-facing split cb02109 installed: mounted count == the
// number of ADVERTISED tools that modelFacing keeps, every mounted tool is non-deferred
// and namespaced memory__*, and every tool hidden by hiddenFromModel is absent from the
// registry while still advertised by the live sidecar (hidden, not removed).
func TestMemoryLiveMount(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	server := liveMemoryServer(endpoint)

	// Ground truth: the live tools/list, partitioned by the SAME modelFacing predicate the
	// bridge uses, so the expected mount count tracks the code instead of a magic number.
	cli, err := mcp.OpenServer(ctx, "memory-probe", server)
	if err != nil {
		t.Fatalf("open streamable-http memory MCP at %s: %v", endpoint, err)
	}
	defs, err := cli.ListTools(ctx)
	if err != nil {
		_ = cli.Close()
		t.Fatalf("tools/list: %v", err)
	}
	_ = cli.Close()
	if len(defs) == 0 {
		t.Fatalf("live sidecar advertised 0 tools — expected the agent-memory surface")
	}

	var wantModel, wantHidden []string
	for _, d := range defs {
		if modelFacing("memory", d.Name) {
			wantModel = append(wantModel, namespacedName("memory", d.Name))
		} else {
			wantHidden = append(wantHidden, namespacedName("memory", d.Name))
		}
	}
	// The split must be non-trivial in BOTH directions or the test proves nothing: the six
	// model verbs must reach the model, and the sidecar must still advertise the hidden set.
	if len(wantHidden) == 0 {
		t.Fatalf("live sidecar advertised none of the hidden tools — hide-not-remove is untestable (advertised %d)", len(defs))
	}

	reg := tools.NewRegistry()
	closer, mounted, err := MountManagedServer(ctx, ctx, reg, "memory", server)
	if err != nil {
		t.Fatalf("MountManagedServer: %v", err)
	}
	defer func() { _ = closer() }()

	if len(mounted) != len(wantModel) {
		t.Fatalf("mounted %d tools, want %d model-facing (hidden set must stay off the registry, served set is %d)",
			len(mounted), len(wantModel), len(defs))
	}

	// Count parity alone can pass with a same-sized but wrong visible set, so require
	// every model-facing name the live listing yields to be registered by name.
	for _, name := range wantModel {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("model-facing tool %q missing from registry", name)
		}
	}

	for _, name := range mounted {
		if !strings.HasPrefix(name, "memory__") {
			t.Errorf("mounted tool %q is not namespaced memory__* (D-07)", name)
		}
		tl, ok := reg.Get(name)
		if !ok {
			t.Fatalf("mounted tool %q missing from registry", name)
		}
		if tl.Spec().Deferred {
			t.Errorf("mounted tool %q is Deferred; model-facing memory tools must be in the default manifest", name)
		}
	}

	// Hidden, not removed: each tool the sidecar still serves but the bridge hides must be
	// absent from the model's registry (Aura reaches it through direct CallTool instead).
	for _, name := range wantHidden {
		if _, ok := reg.Get(name); ok {
			t.Errorf("hidden tool %q reached the model registry — cb02109 hides it, the sidecar still serves it", name)
		}
	}

	// Spot-check the recall tool the agent loop reaches via tool_search is present.
	if _, ok := reg.Get("memory__memory_search"); !ok {
		t.Errorf("memory__memory_search not registered — the recall tool must mount")
	}
}
