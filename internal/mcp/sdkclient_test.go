package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// echoIn/echoOut give the SDK a schema to derive for the fixture server's one tool.
type echoIn struct {
	Text string `json:"text"`
}

type echoOut struct {
	Text string `json:"text"`
}

// newSDKFixtureServer builds a real SDK server advertising toolCount trivial tools.
// Real, not a hand-written JSON-RPC fake: the point of this phase is that Aura no
// longer owns wire framing, so a fixture that framed its own would prove nothing.
func newSDKFixtureServer(toolCount int) *sdkmcp.Server {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "1.0.0"}, nil)
	for i := range toolCount {
		name := "echo"
		if i > 0 {
			name = "echo_" + string(rune('a'+i))
		}
		sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: name, Description: "echoes its input"},
			func(_ context.Context, _ *sdkmcp.CallToolRequest, in echoIn) (*sdkmcp.CallToolResult, echoOut, error) {
				return nil, echoOut(in), nil
			})
	}
	return srv
}

// recordingHandler captures slog records so a test can assert the exact keys of the
// one record an opened session must emit.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) find(msg string) (slog.Record, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var found slog.Record
	count := 0
	for _, r := range h.records {
		if r.Message == msg {
			found = r
			count++
		}
	}
	return found, count
}

func recordAttrs(r slog.Record) map[string]string {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	return attrs
}

// startSDKHTTPServer serves a real streamable-HTTP MCP endpoint over httptest and
// returns the server plus a counter of HTTP requests it actually received.
func startSDKHTTPServer(t *testing.T, toolCount int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	srv := newSDKFixtureServer(toolCount)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)
	var requests atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		// The handler owns a server-side session per client, and its closeAll is
		// unexported — so a client-side Close alone leaves the server's connection
		// reader goroutine alive and goleak fails the whole package. Close the server
		// side explicitly, before shutting the listener.
		for session := range srv.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return ts, &requests
}

// recipeServer is a trusted built-in recipe pointed at url — the one shape whose
// enforced egress policy may still reach a loopback authority.
func recipeServer(url string) ManagedServer {
	return ManagedServer{Type: ServerTypeStreamableHTTP, URL: url, Source: SourceRecipeMemory}
}

// TestOpenSDKSessionStreamableHTTP proves the HTTP branch opens a live session under an
// ENFORCED egress policy and that the negotiated protocol version is logged exactly once.
func TestOpenSDKSessionStreamableHTTP(t *testing.T) {
	ts, requests := startSDKHTTPServer(t, 1)
	server := recipeServer(ts.URL)
	egress := EgressPolicyForManagedServer(true, server)
	if !egress.Enforced() {
		t.Fatal("fixture policy must be enforced for this test to mean anything")
	}

	rec := &recordingHandler{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := OpenSDKSession(ctx, "memory", server, egress, SessionOptions{Logger: slog.New(rec)})
	if err != nil {
		t.Fatalf("OpenSDKSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	if got := requests.Load(); got == 0 {
		t.Error("want at least one HTTP request against the fixture server, got 0")
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 1 {
		t.Errorf("ListTools: want 1 tool, got %d", len(tools.Tools))
	}

	record, count := rec.find("mcp session open")
	if count != 1 {
		t.Fatalf(`want exactly one "mcp session open" record, got %d`, count)
	}
	if record.Level != slog.LevelInfo {
		t.Errorf("record level: want INFO, got %v", record.Level)
	}
	attrs := recordAttrs(record)
	for _, key := range []string{"server", "transport", "protocol_version", "session_id"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("record is missing key %q (got %v)", key, attrs)
		}
	}
	if attrs["server"] != "memory" {
		t.Errorf(`attr server: want "memory", got %q`, attrs["server"])
	}
	if attrs["transport"] != "http" {
		t.Errorf(`attr transport: want "http", got %q`, attrs["transport"])
	}
	if attrs["protocol_version"] == "" {
		t.Error("attr protocol_version: want the negotiated version, got empty")
	}
}

// TestOpenSDKSessionRefusesPolicyBlockedEndpoint proves the endpoint guard runs BEFORE
// the transport is built: a refused endpoint issues ZERO HTTP requests and the error is
// classifiable as a transport error so MountWithRetry still treats it as retryable.
func TestOpenSDKSessionRefusesPolicyBlockedEndpoint(t *testing.T) {
	ts, requests := startSDKHTTPServer(t, 1)
	// Not a recipe source: an enforced policy grants this loopback authority nothing.
	server := ManagedServer{Type: ServerTypeStreamableHTTP, URL: ts.URL, Trust: ManagedTrust{Class: TrustRemoteHTTP}}
	egress := EgressPolicy{enforcePrivate: true}

	_, err := OpenSDKSession(context.Background(), "blocked", server, egress, SessionOptions{})
	if err == nil {
		t.Fatal("OpenSDKSession: want an error for a policy-refused endpoint, got nil")
	}
	if !IsTransportError(err) {
		t.Errorf("IsTransportError: want true so MountWithRetry retries, got false (err=%v)", err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("want ZERO HTTP requests against a refused endpoint, got %d", got)
	}
}

// TestOpenSDKSessionRefusesBlockedRedirect proves the redirect guard survived relocation:
// a 302 into cloud-metadata space is refused mid-chain, not followed.
func TestOpenSDKSessionRefusesBlockedRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer ts.Close()

	// Zero policy: the initial loopback endpoint passes, so the ONLY thing that can
	// refuse this is the redirect guard itself.
	server := ManagedServer{Type: ServerTypeStreamableHTTP, URL: ts.URL, Trust: ManagedTrust{Class: TrustRemoteHTTP}}

	_, err := OpenSDKSession(context.Background(), "redirector", server, EgressPolicy{}, SessionOptions{})
	if err == nil {
		t.Fatal("OpenSDKSession: want an error for a redirect into metadata space, got nil")
	}
	if !strings.Contains(err.Error(), "mcp redirect blocked") {
		t.Errorf(`error text: want it to contain "mcp redirect blocked", got %q`, err.Error())
	}
}

// TestOpenSDKSessionRejectsMixedClassify proves the F-027 gate holds on the SDK path: a
// mixed url+command entry returns Classify's own error and never spawns a subprocess.
func TestOpenSDKSessionRejectsMixedClassify(t *testing.T) {
	server := ManagedServer{URL: "http://127.0.0.1:1/mcp", Command: "this-binary-does-not-exist-45-1"}

	_, err := OpenSDKSession(context.Background(), "mixed", server, EgressPolicy{}, SessionOptions{})
	if err == nil {
		t.Fatal("OpenSDKSession: want Classify's rejection for a mixed entry, got nil")
	}
	if !strings.Contains(err.Error(), "mcp classify") {
		t.Errorf("want Classify's own error, got %q", err.Error())
	}
}

// TestOpenSDKSessionForConfigStdio proves a stdio dial failure is classifiable as a
// transport error — the property mount_retry.go's boot-time retry depends on.
func TestOpenSDKSessionForConfigStdio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := OpenSDKSessionForConfig(ctx, ctx, "missing", ServerConfig{Command: "this-binary-does-not-exist-45-1"}, SessionOptions{})
	if err == nil {
		t.Fatal("OpenSDKSessionForConfig: want an error for a non-existent command, got nil")
	}
	if !IsTransportError(err) {
		t.Errorf("IsTransportError: want true so MountWithRetry retries, got false (err=%v)", err)
	}
}

// TestSDKClientOptionsDefaults pins the two option values that are not observable on
// the wire. KeepAlive stays unset because it is inert on protocol >= 2026-07-28
// (SEP-2575) and would silently do nothing on a modern peer while looking like liveness.
func TestSDKClientOptionsDefaults(t *testing.T) {
	opts := SDKClientOptions(SessionOptions{})
	if opts == nil {
		t.Fatal("SDKClientOptions: want non-nil options")
	}
	if opts.Capabilities == nil {
		t.Error("Capabilities: want non-nil so the SDK's historical default is overridden, got nil")
	}
	if opts.Logger == nil {
		t.Error("Logger: want a non-nil default, got nil")
	}
	if opts.KeepAlive != 0 {
		t.Errorf("KeepAlive: want unset (inert on protocol >= 2026-07-28), got %v", opts.KeepAlive)
	}
}

// TestSDKClientOptionsWiresHandlers proves the seam later plans fill is actually wired.
func TestSDKClientOptionsWiresHandlers(t *testing.T) {
	opts := SDKClientOptions(SessionOptions{
		ToolListChanged: func(context.Context, *sdkmcp.ToolListChangedRequest) {},
		Elicitation: func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
			return nil, nil
		},
	})
	if opts == nil {
		t.Fatal("SDKClientOptions: want non-nil options")
	}
	if opts.ToolListChangedHandler == nil {
		t.Error("ToolListChangedHandler: want it wired from SessionOptions, got nil")
	}
	if opts.ElicitationHandler == nil {
		t.Error("ElicitationHandler: want it wired from SessionOptions, got nil")
	}
}

// TestOpenSDKSessionInMemoryRoundTrip proves a client built from SDKClientOptions
// completes a real round trip against a real server over the SDK's in-memory pair —
// daemon-free, no subprocess, no socket — and that what it ADVERTISED on the way in
// carries no capability Aura does not implement.
//
// The capability check reads the server's view rather than the client's struct: the
// wire is the contract, and the SDK infers capabilities at connect time from handlers
// as well as from ClientOptions.Capabilities, so a struct-field assertion would miss
// an inferred one. It also keeps the test off identifiers SEP-2577 deprecated.
func TestOpenSDKSessionInMemoryRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := newSDKFixtureServer(1).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	session, err := newSDKClient(SessionOptions{}).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 1 {
		t.Fatalf("ListTools: want 1 tool, got %d", len(tools.Tools))
	}
	if tools.Tools[0].Name != "echo" {
		t.Errorf("tool name: want echo, got %q", tools.Tools[0].Name)
	}

	params := serverSession.InitializeParams()
	if params == nil {
		t.Fatal("server saw no InitializeParams")
	}
	if params.ClientInfo == nil || params.ClientInfo.Name != mcpClientName {
		t.Errorf("clientInfo.name: want %q, got %+v", mcpClientName, params.ClientInfo)
	}
	advertised, err := json.Marshal(params.Capabilities)
	if err != nil {
		t.Fatalf("marshal advertised capabilities: %v", err)
	}
	// Measured against go-sdk v1.7.0, because the SDK's own doc overstates this: it
	// says &ClientCapabilities{} "disables the roots capability", but the key is a
	// struct VALUE tagged `json:"roots,omitempty"`, and encoding/json ignores
	// omitempty for struct values. So "roots":{} is on the wire unconditionally and
	// no option can remove it.
	//
	// What the empty value genuinely buys is the part that matters: the
	// advertisement drops from {"roots":{"listChanged":true}} to {"roots":{}}, and
	// listChanged is what a server actually reads to decide behaviour (go-sdk
	// v1.7.0 mcp/client.go:712-716). Assert the effect, not the doc's wording.
	if strings.Contains(string(advertised), "listChanged") {
		t.Errorf("advertised roots.listChanged, which Aura does not implement: %s", advertised)
	}
	// These two are pointer fields, so absent means absent. Both are deprecated by
	// SEP-2577 and Aura implements neither.
	for _, capability := range []string{"sampling", "logging"} {
		if strings.Contains(string(advertised), capability) {
			t.Errorf("advertised capability %q that Aura does not implement: %s", capability, advertised)
		}
	}
}

// TestTransportErrorfClassifies proves the single place SDK failures acquire Aura's
// transport classification actually does so — SDK errors never wrap ErrTransport
// themselves, so without this every dial failure would look permanent to MountWithRetry.
func TestTransportErrorfClassifies(t *testing.T) {
	base := context.DeadlineExceeded
	err := TransportErrorf("srv", base)
	if !IsTransportError(err) {
		t.Error("IsTransportError: want true, got false")
	}
	if !strings.Contains(err.Error(), `"srv"`) {
		t.Errorf("want the server name in the message, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), base.Error()) {
		t.Errorf("want the cause preserved, got %q", err.Error())
	}
}
