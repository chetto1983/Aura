package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/procgroup"
	"github.com/chetto1983/aura/internal/redact"
)

// sdkclient.go is the ONE place Aura constructs an MCP client. Everything that used
// to be hand-rolled underneath — JSON-RPC framing, request-id bookkeeping, the
// initialize handshake, session state — now belongs to the SDK. What stays Aura's is
// the part the SDK has no opinion about: which transport a configured server resolves
// to, and the network policy that transport must carry.
//
// A second construction point would be a second security posture, so there is exactly
// one: the agent bridge and the diagnostic probe open sessions through this function,
// which is why a policy fix in one cannot miss the other.

const (
	mcpClientName    = "aura"
	mcpClientVersion = "2.1.0"
)

// Transport labels for logs and metric dimensions. They deliberately match the values
// the pre-SDK boundaries already used: the observable surface must not shift just
// because the implementation underneath changed.
const (
	transportLabelStdio = "stdio"
	transportLabelHTTP  = "http"
)

// SessionOptions is the seam later plans fill: sending middleware, the
// tool-list-changed notification, and elicitation (45.1-07). Every field is
// usable now.
//
// Corrected 2026-08-27 (Phase 51, D-10): this comment used to say "45.1-05
// stamps the calling identity onto _meta" -- that was the plan at seam-creation
// time, but internal/agent/mcptools.IdentityBindingMiddleware's own doc comment
// records the shipped decision: "nothing proprietary is written to MCP request
// metadata". Identity selection rides the standard bearer `sub` instead.
// Headers/HeaderFunc are the seam Phase 51 actually uses for a host-derived
// value the far side must read beside `sub`: connection-scoped HTTP headers
// (below _meta, at the transport layer), never JSON-RPC request metadata.
type SessionOptions struct {
	Logger          *slog.Logger
	Sending         []sdkmcp.Middleware
	ToolListChanged func(context.Context, *sdkmcp.ToolListChangedRequest)
	Elicitation     func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error)
	// Headers are extra static HTTP headers attached to every request this
	// session's underlying transport sends, applied via the SAME
	// withStaticHeaders/headerRoundTripper mechanism MCP_HEADER_* env entries
	// already use (httpAuthFromEnv) -- not a new customization surface, the
	// existing one, given a second caller. nil/empty adds nothing. Streamable-HTTP
	// only: OpenSDKSessionForConfig's stdio branch has no HTTP transport to attach
	// headers to.
	Headers map[string]string
	// HeaderFunc, when set, is called with EACH request's own ctx and its
	// return value is applied the same way Headers is -- the ctx-parameterized
	// sibling Headers cannot be: a session opened once and reused across many
	// calls (internal/agent/mcptools' identity-scoped session, shared across
	// every turn an identity runs) cannot bake a per-call-accurate value in at
	// connect time without going stale the moment a second turn starts. This
	// package stays oblivious to what the returned headers MEAN (D-10's
	// run-id/writer-role actor is internal/agent/mcptools' concept, not this
	// package's) -- it only knows to call the function and apply what comes
	// back, the same contract Sending already has for opaque middleware.
	// nil/empty return adds nothing for that request. Streamable-HTTP only,
	// same reason as Headers.
	HeaderFunc func(context.Context) map[string]string
	// OAuth carries the per-identity authorization wiring for remote servers. Its zero
	// value is meaningful: no store and no fetcher still attaches a handler, so a
	// server that needs authorization says so instead of returning a bare 401.
	OAuth OAuthOptions
}

func resolveLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// SDKClientOptions builds the client options every Aura MCP session is opened with.
//
// Deliberately unset: KeepAlive (inert on protocol >= 2026-07-28), the sampling and
// logging handlers (deprecated by SEP-2577), and MultiRoundTrip — left nil the SDK
// enables MRTR with its defaults, which is what plan 45.1-07's elicitation needs.
func SDKClientOptions(o SessionOptions) *sdkmcp.ClientOptions {
	opts := &sdkmcp.ClientOptions{
		Logger: resolveLogger(o.Logger),
		// Load-bearing. Left nil, the SDK advertises {"roots":{"listChanged":true}}
		// "for historical reasons" (go-sdk v1.7.0, ClientOptions.Capabilities doc) —
		// announcing a capability Aura does not implement. An empty value is the
		// documented way to disable roots outright; the doc also notes Capabilities.Roots
		// is ignored, so setting the empty struct is the whole mechanism.
		Capabilities: &sdkmcp.ClientCapabilities{},
	}
	if o.ToolListChanged != nil {
		opts.ToolListChangedHandler = o.ToolListChanged
	}
	if o.Elicitation != nil {
		opts.ElicitationHandler = o.Elicitation
	}
	return opts
}

func newSDKClient(o SessionOptions) *sdkmcp.Client {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: mcpClientName, Version: mcpClientVersion}, SDKClientOptions(o))
	if len(o.Sending) > 0 {
		client.AddSendingMiddleware(o.Sending...)
	}
	return client
}

// OpenSDKSession opens a live session to one managed MCP server over whichever
// transport Classify resolves, with Aura's network policy attached.
//
// Classify runs FIRST and its error is returned unchanged. That ordering is the F-027
// gate: a mixed or inconsistent entry must die here, never fall through to a
// subprocess spawn.
func OpenSDKSession(ctx context.Context, name string, server ManagedServer, egress EgressPolicy, o SessionOptions) (*sdkmcp.ClientSession, error) {
	serverType, _, err := Classify(server)
	if err != nil {
		return nil, fmt.Errorf("mcp open %q: %w", name, err)
	}
	if serverType == ServerTypeStreamableHTTP {
		return openSDKHTTP(ctx, name, server, egress, o)
	}
	return OpenSDKSessionForConfig(ctx, ctx, name, ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env}, o)
}

// openSDKHTTP builds the streamable-HTTP transport with the full policy stack on its
// http.Client. The order is the security property: the endpoint is validated before a
// transport exists at all, the hardened dialer resolves-then-pins underneath, the
// redirect guard re-validates every hop, and credentials go on last so they can never
// ride a request to a host the policy already refused.
func openSDKHTTP(ctx context.Context, name string, server ManagedServer, egress EgressPolicy, o SessionOptions) (cs *sdkmcp.ClientSession, err error) {
	ctx, end := sdkHTTPConnectBoundary.Start(ctx)
	defer end.PanicSafe(&err)

	validated, err := guardEndpointWithPolicy(ctx, server.URL, egress, net.DefaultResolver)
	if err != nil {
		return nil, TransportErrorf(name, err)
	}

	httpClient := oauthHTTPClient(egress)

	// The authorization flow is built from the client as it stands HERE, before the
	// static headers go on: its requests leave for the authorization server, a
	// different origin, and headerRoundTripper applies MCP_HEADER_* with no origin
	// check. See oauthHandlerFor.
	settings, err := OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return nil, TransportErrorf(name, err)
	}
	oauthHandler, err := oauthHandlerFor(ctx, name, server, settings, httpClient, o.OAuth, o.Logger)
	if err != nil {
		return nil, TransportErrorf(name, err)
	}

	headers, _ := httpAuthFromEnv(server.Env)
	httpClient = withStaticHeaders(httpClient, headers)
	// SessionOptions.Headers rides the SAME wrapper as MCP_HEADER_* -- a second
	// caller of an existing seam, not a new one. Applied after the operator's own
	// static headers so a caller-supplied actor header (D-10) cannot be shadowed
	// by an env-declared one sharing the same name; distinct header names in
	// practice, ordering recorded for when they are not.
	httpClient = withStaticHeaders(httpClient, o.Headers)
	// HeaderFunc runs last and per-request: it is how a REUSED session (one
	// identity's session, shared across many different turns) stays accurate
	// call-to-call, which a value fixed here at connect time cannot.
	httpClient = withContextHeaders(httpClient, o.HeaderFunc)

	// Bounded because the SDK is not: see bounded_call.go.
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:     validated.String(),
		HTTPClient:   httpClient,
		OAuthHandler: oauthHandler,
	}
	client := newSDKClient(o)
	session, err := BoundedCall(ctx,
		func(ctx context.Context) (*sdkmcp.ClientSession, error) { return client.Connect(ctx, transport, nil) },
		closeSession)
	if err != nil {
		return nil, TransportErrorf(name, err)
	}
	logNegotiatedProtocol(resolveLogger(o.Logger), name, transportLabelHTTP, session)
	return session, nil
}

// OpenSDKSessionForConfig opens a stdio session for a declared launch config.
//
// The two contexts are not redundant (Pitfall #2): processCtx bounds the subprocess's
// ENTIRE lifetime and must be long-lived, while handshakeCtx bounds only the connect
// round-trip. Collapsing them into one is how a single mount's handshake deadline
// later kills every healthy server sharing that context.
//
// D-106: the launch-command validator that used to live here — shell-metacharacter
// rejection, relative-path rejection, shell-interpreter rejection — is deliberately
// NOT ported, and still is not. That was an explicit operator decision on the grounds
// that mounting must cost no ceremony, and it stands.
//
// What DID land (2026-08-23) is checkStdioShape: not an allowlist, not a ban on any
// interpreter, but a refusal of the two or three launch shapes no MCP server has. The
// premise the D-106 acceptance rested on — that only the operator writes the registry
// — was measured false: a skill install runs third-party npm lifecycle scripts as root
// in this container, and the registry is writable from there with the database
// credentials this container's environment holds. See stdio_shape.go for why the answer
// is persistence-denial rather than validation.
func OpenSDKSessionForConfig(processCtx, handshakeCtx context.Context, name string, cfg ServerConfig, o SessionOptions) (cs *sdkmcp.ClientSession, err error) {
	handshakeCtx, end := sdkStdioConnectBoundary.Start(handshakeCtx)
	defer end.PanicSafe(&err)

	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		// Not a transport error: an empty command is a config defect that retrying
		// cannot fix, so MountWithRetry must not treat it as a boot-timing failure.
		return nil, fmt.Errorf("mcp %q: empty command", name)
	}
	// The spawn-time half of stdio_shape.go, and the one that matters: an entry
	// planted out of band never passed through SaveManagedConfig, so this is the only
	// checkpoint standing between it and exec. Returned unwrapped for the same reason
	// as the empty command above — a shape refusal is permanent, never a boot-timing
	// failure for MountWithRetry to sit through.
	if err := checkStdioShape(name, command, cfg.Args, cfg.Env); err != nil {
		return nil, err
	}
	// G204: Command/Args/Env come from the operator-controlled mcpServers config,
	// not from untrusted model output.
	cmd := exec.CommandContext(processCtx, command, cfg.Args...) //nolint:gosec
	cmd.Env = processEnvForMCP(cfg.Env)
	// D-10: the child leads its own process group before it starts, so a later kill
	// reaps the WHOLE spawned tree instead of leaking grandchildren (F-035).
	procgroup.SetProcessGroup(cmd)

	// Bounded for the same reason as the HTTP branch (see connect_bounded.go): a peer
	// that accepts the connection and then never answers the handshake must cost this
	// caller handshakeCtx and no more. On stdio that peer is a child process Aura just
	// spawned, so an unbounded wait here is a mount that never returns.
	transport := &sdkmcp.CommandTransport{Command: cmd}
	client := newSDKClient(o)
	session, err := BoundedCall(handshakeCtx,
		func(ctx context.Context) (*sdkmcp.ClientSession, error) { return client.Connect(ctx, transport, nil) },
		closeSession)
	if err != nil {
		return nil, TransportErrorf(name, err)
	}
	logNegotiatedProtocol(resolveLogger(o.Logger), name, transportLabelStdio, session)
	return session, nil
}

// logNegotiatedProtocol records what each peer actually negotiated. Keep this line
// permanently: the fleet's protocol versions are otherwise an assumption, and they
// decide whether any peer could ever use KeepAlive (inert from 2026-07-28 onward).
//
// Both values are read defensively. Protocol 2026-07-28 removes the initialize
// exchange and the Mcp-Session-Id header entirely (SEP-2575), so on a modern peer
// there may be no InitializeResult and no session id to report — that is a fact about
// the peer worth logging as empty, not a failure.
func logNegotiatedProtocol(logger *slog.Logger, name, transport string, cs *sdkmcp.ClientSession) {
	if cs == nil {
		return
	}
	version := ""
	if result := cs.InitializeResult(); result != nil {
		version = result.ProtocolVersion
	}
	logger.Info("mcp session open",
		"server", redact.Line(name),
		"transport", transport,
		"protocol_version", version,
		"session_id", cs.ID(),
	)
}

// headerRoundTripper adds operator-declared non-auth headers to every request.
// StreamableClientTransport has no generic header field, so this is where MCP_HEADER_*
// entries land. Bearer tokens use OAuthHandler, the SDK's authorization seam.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone: RoundTrip must not mutate the request it was handed.
	clone := req.Clone(req.Context())
	for key, value := range h.headers {
		clone.Header.Set(key, value)
	}
	return h.base.RoundTrip(clone)
}

// withStaticHeaders wraps client's transport so non-auth headers ride every request. It
// wraps the transport rather than replacing it, so the hardened dialer survives.
func withStaticHeaders(base *http.Client, headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return base
	}
	cloned := *base
	inner := cloned.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	cloned.Transport = &headerRoundTripper{base: inner, headers: headers}
	return &cloned
}

// ctxHeaderRoundTripper is headerRoundTripper's ctx-parameterized sibling
// (SessionOptions.HeaderFunc): headers are computed FRESH from each request's
// own context rather than fixed once when the client was built, which is what
// a REUSED session across many different calls (D-10, Phase 51) needs.
type ctxHeaderRoundTripper struct {
	base http.RoundTripper
	fn   func(context.Context) map[string]string
}

func (h *ctxHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	headers := h.fn(req.Context())
	if len(headers) == 0 {
		return h.base.RoundTrip(req)
	}
	// Clone: RoundTrip must not mutate the request it was handed.
	clone := req.Clone(req.Context())
	for key, value := range headers {
		clone.Header.Set(key, value)
	}
	return h.base.RoundTrip(clone)
}

// withContextHeaders wraps client's transport so fn's per-request headers ride
// every request, in addition to (and applied after) any withStaticHeaders
// wrapping already on base.
func withContextHeaders(base *http.Client, fn func(context.Context) map[string]string) *http.Client {
	if fn == nil {
		return base
	}
	cloned := *base
	inner := cloned.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	cloned.Transport = &ctxHeaderRoundTripper{base: inner, fn: fn}
	return &cloned
}

// ssrfEnforceFromEnv reads the AURA_MCP_SSRF_ENFORCE knob (AURA_<DOMAIN>_<UNIT>).
// Default OFF: an unset/empty/false value keeps lenient profiles compatible with
// local sidecars. RuntimeEgressPolicy ORs this with strict-profile enforcement,
// so the knob can tighten a lenient profile but cannot weaken a strict one.
func ssrfEnforceFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AURA_MCP_SSRF_ENFORCE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// httpAuthFromEnv derives static headers and a bearer token from an operator-declared
// managed-server Env. Credentials come from here and nowhere else.
func httpAuthFromEnv(env []string) (map[string]string, string) {
	headers := map[string]string{}
	bearer := ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch {
		case key == "MCP_BEARER_TOKEN":
			bearer = value
		case strings.HasPrefix(key, "MCP_HEADER_"):
			header := strings.ReplaceAll(strings.TrimPrefix(key, "MCP_HEADER_"), "_", "-")
			if header != "" {
				headers[header] = value
			}
		}
	}
	if len(headers) == 0 {
		headers = nil
	}
	return headers, bearer
}
