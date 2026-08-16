# API Coverage — modelcontextprotocol/go-sdk v1.7.0 (client half)

> Full coverage by default. Opt-outs are explicit, reasoned decisions.
>
> **Why this file exists.** The operator's directive for this phase was *"no bespoke —
> look what native mcp can give us"*. This matrix is that directive made auditable: it is
> the **subtraction record** for the SDK's client surface. Anything Aura writes by hand
> after this phase must be absent from this table's `INTEGRATE` column, or the table is
> wrong.
>
> **Method.** Enumerated field-by-field and method-by-method from the pinned module cache
> at `$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.7.0/mcp`, the literal
> artifact `go build` uses. Every row cites a line. Where a doc comment and the shipped
> code disagree, the code wins (that rule produced the `KeepAlive` row).
>
> Enumerated 2026-08-16 against v1.7.0. Re-run if the pin moves.

## Session lifecycle

| capability | decision | reason |
|---|---|---|
| `NewClient` + `ClientOptions` (`client.go:51`) | INTEGRATE | `internal/mcp.SDKClientOptions` is the single construction point; every mount and every CLI probe goes through it. |
| `Client.Connect(ctx, Transport, *ClientSessionOptions)` (`client.go:307`) | INTEGRATE | Replaces `Open`/`OpenHTTP`. Owns `initialize` / `server/discover` negotiation — Aura's `protocol.go` deletes into it. |
| `ClientSession.InitializeResult()` (`client.go:506`) | INTEGRATE | `.ProtocolVersion` logged per mounted server at connect, permanently. This is the fact that decides whether any peer could ever use `KeepAlive`. Dissolves RESEARCH Open Question #4 without a spike. |
| `ClientSession.ID()` (`client.go:547`) | INTEGRATE | Logged alongside the negotiated version. Empty under 2026-07-28 (SEP-2567 removed `Mcp-Session-Id`) — that emptiness is itself the evidence the stateless core is live. |
| `ClientSession.Close()` (`client.go:559`) | INTEGRATE | The mount closer. Idempotent and concurrency-safe; cancels keepalive, listeners and resource subscriptions. |
| `ClientSession.Wait()` (`client.go:584`) | INTEGRATE | **The native death signal.** One goroutine per session blocked on `Wait()` replaces `bridge_ping.go`'s 113-LOC poll loop entirely: push, not poll; no interval, no missed window, no ping the peer may refuse. |
| `ClientSession.Ping(ctx, *PingParams)` (`client.go:1222`) | OPT-OUT | Refused by any 2026-07-28 peer: `server.go:1879-1887` answers `methodPing` with `CodeMethodNotFound` when `usesNewProtocol`. `Wait()` supersedes it and is strictly more accurate. Kept off rather than kept as a second, misleading signal. |
| `ClientOptions.KeepAlive` / `KeepAliveFailureThreshold` (`client.go` opts) | OPT-OUT | **Inert and silent against a 2026-07-28 peer.** No client-side version gate (`client.go:403` is a bare `if c.opts.KeepAlive > 0`), the SDK server refuses the ping (`server.go:1879-1887`), and the keepalive goroutine then retires itself without closing the session or raising an error (`shared.go:869-872`). Setting it would pass code review and do nothing. |
| Session redial policy (when / how often / with what backoff) | NOT IN SDK — Aura keeps it | No MCP library can decide a host's supervision policy. `mountedServer` owns it: breaker (3 failures / 30s cooldown) + capped exponential backoff, carried over from `bridge_reconnect.go`. Contains no JSON-RPC framing, no session model, no wire handling — policy, not protocol. |

## Tools

| capability | decision | reason |
|---|---|---|
| `tools/list` — `ClientSession.ListTools` (`client.go:1257`) | INTEGRATE | The mount-time and probe-time discovery call. |
| `tools/list` pagination — `ClientSession.Tools(...) iter.Seq2[*Tool, error]` (`client.go:1549`) | INTEGRATE | A real capability gain: Aura's hand-rolled `listToolsWith` had **no** cursor handling at all. `probe.go`'s tool count drains this iterator. |
| `tools/call` — `ClientSession.CallTool` (`client.go:1278`) | INTEGRATE | `bridgedTool.Execute`'s new call target. |
| `CallToolResult.IsError` + `.Content` + `.StructuredContent` (`protocol.go` `CallToolResult`) | INTEGRATE | The typed replacement for `decodeToolResult`'s raw-envelope parse. |
| `ToolAnnotations` — all four hints (`protocol.go` `ToolAnnotations`) | INTEGRATE | D-107. Aura's own `ToolAnnotations` carried only two (`ReadOnlyHint`, `DestructiveHint`); deleting it removes the truncation for free. `IdempotentHint` is the spec's own signal for what `applyMCPOperationMetadata` hardcodes today. |
| `ClientOptions.ToolListChangedHandler` (`client.go` opts, dispatched `client.go:337`) | INTEGRATE | D-104's drift check. Server-pushed, so drift is noticed when it happens rather than on the next reconnect. |
| Tool-set drift → mark-server-dead semantics | NOT IN SDK — Aura keeps it | `tools.Registry` is immutable once a run starts (no lock, no `Unregister`, `internal/agent/tools/spec.go:183-213`). The SDK has no opinion about a host registry it cannot see. The `"tool set changed; restart required"` failure shape is preserved verbatim. |

## Server→client requests and notifications

| capability | decision | reason |
|---|---|---|
| `ClientOptions.ElicitationHandler` (form mode) | INTEGRATE | MCPC-05. Setting it non-nil auto-advertises the elicitation capability. Today, with it nil, `c.elicit` returns `CodeInvalidParams "client does not support elicitation"` (`client.go:862-864`) — already fail-closed, so this closes no hole; it makes a legitimate SEP-2322 multi-round-trip tool possible at all. |
| Elicitation **URL mode** (`ElicitParams.Mode == "url"`, `.URL`, `.ElicitationID`) | OPT-OUT | Honouring it means opening a browser at a server-supplied URL and waiting for `notifications/elicitation/complete` — an out-of-band flow with no operator surface in Aura and an obvious phishing shape. Declined cleanly (`Action: "decline"`) so the server does not hang. Hermes made the same call for the same reason (`tools/mcp_tool.py:1720-1731`). |
| `ClientOptions.ElicitationCompleteHandler` | OPT-OUT | Only meaningful for URL-mode elicitation, which is OPT-OUT above. |
| `ClientOptions.MultiRoundTrip` (MRTR, SEP-2322) | INTEGRATE (as default) | Enabled by default when nil (`client.go:79-81`). Aura leaves it enabled and writes **only** the handler body — `clientMultiRoundTripMiddleware` (`mrtr.go:73-115`) already owns the fulfil-and-retry loop. Explicitly do NOT set `Disabled`. |
| `ClientOptions.ProgressNotificationHandler` | OPT-OUT | No Aura surface renders mid-call progress today; wiring it would produce log noise with no consumer. Re-open when the cockpit grows a per-call progress affordance. |
| `ClientSession.NotifyProgress` (`client.go:1541`) | OPT-OUT | Client→server progress. Aura is not a long-running responder to a server; nothing to report. |
| `ClientOptions.PromptListChangedHandler` | OPT-OUT | Prompts are OPT-OUT (below); a change notification for an unused feature has nothing to invalidate. |
| `ClientOptions.ResourceListChangedHandler` / `ResourceUpdatedHandler` | OPT-OUT | Resources are OPT-OUT (below), same reason. |
| `ClientOptions.LoggingMessageHandler` | OPT-OUT | **Deprecated as of 2026-07-28 (SEP-2577).** Aura reads sidecar stderr and its own `slog`; adopting a deprecated channel now buys a migration later. |
| `ClientOptions.CreateMessageHandler` / `CreateMessageWithToolsHandler` (sampling) | OPT-OUT | **Deprecated as of 2026-07-28 (SEP-2577).** Aura calls LLM providers directly through `internal/llm`; letting a mounted server drive Aura's model is a budget and prompt-injection surface with no requirement behind it. |
| `Client.AddRoots` / `RemoveRoots` (`client.go:660`, `:678`) | OPT-OUT | **Deprecated as of 2026-07-28 (SEP-2577).** Aura's filesystem scope is enforced by the sandbox and the fs tools, not advertised to a server. |
| `ClientOptions.Capabilities` | INTEGRATE | Set explicitly to `&sdkmcp.ClientCapabilities{}` so the SDK's historical default `{"roots":{"listChanged":true}}` (`client.go:262-269`) is **not** advertised. Aura must not announce a capability it opted out of — that is a silent security-relevant default, and it is the only reason this row is not "leave nil". |

## Other feature families

| capability | decision | reason |
|---|---|---|
| Prompts — `ListPrompts` / `GetPrompt` / `Prompts` (`client.go:1231`, `:1249`, `:1588`) | OPT-OUT | No mounted server advertises prompts, and Aura's prompt surface is `Agent.md` + skills. Nothing to render them into. |
| Resources — `ListResources` / `ReadResource` / `ListResourceTemplates` / `Resources` / `ResourceTemplates` (`client.go:1309`, `:1345`, `:1327`, `:1562`, `:1575`) | OPT-OUT | Aura's document plane is Garage + Postgres catalogue + ArcadeDB, reached by `document_search`/`document_open`. Mounting MCP resources would give the model two tools with one meaning over a store that holds nothing (the same reasoning `cmd/arcadedb-mcp/main.go:186-190` records for documents). |
| Resource subscriptions — `Subscribe` / `Unsubscribe` (`client.go:1375`, `:1415`) | OPT-OUT | Depends on resources, which are OPT-OUT. |
| Completion — `Complete` (`client.go:1366`) | OPT-OUT | Argument autocompletion is an IDE affordance; Aura's model fills arguments from the schema `tool_search` already returns. |
| `SetLoggingLevel` (`client.go:1303`) | OPT-OUT | **Deprecated (SEP-2577)**, and paired with `LoggingMessageHandler`, also OPT-OUT. |

## Middleware, transports and observability

| capability | decision | reason |
|---|---|---|
| `Client.AddSendingMiddleware` (`client.go:1129`) | INTEGRATE | MCPC-04's seam. Carries `_meta["aura"].{user_identifier, operation_key, operation_scope, operation_fingerprint}`. The SDK's own idiom — MRTR ships as default-on client middleware. |
| `Client.AddReceivingMiddleware` (`client.go:1144`) | OPT-OUT | Aura receives no server→client request it wants to intercept generically; elicitation is handled by its own typed handler, which is a better-typed seam than a `Request`/`Result` middleware. |
| `Meta` / `GetMeta` / `SetMeta` on every `Params` (`shared.go:483-490`) | INTEGRATE | The `_meta` channel. `injectRequestMeta` (`client.go:522`) leaves keys already present untouched, so Aura's `aura` namespace coexists with the SDK's `io.modelcontextprotocol/*` triple without contention. |
| `CommandTransport{Command, TerminateDuration}` (`cmd.go:20`) | INTEGRATE | The stdio branch. **Two fields — no respawn, no restart policy, no supervision.** Confirms D-105: a dead stdio child stays dead until the host redials. |
| `StreamableClientTransport.Endpoint` / `.HTTPClient` (`streamable.go`) | INTEGRATE | MCPC-02's attachment point. `HTTPClient` is a plain `*http.Client`, so `newHardenedHTTPClient` drops in with **no adapter**. |
| `StreamableClientTransport.MaxRetries` (default 5) | INTEGRATE (default) | The real HTTP retry knob. Left at its default; documented so nobody re-implements retry beside it. |
| `StreamableClientTransport.DisableStandaloneSSE` | OPT-OUT (left false) | Under 2026-07-28 the standalone SSE stream is removed unconditionally anyway (`streamable.go:2095-2098`), so setting this would be a no-op that implies a decision nobody made. Left at the default so a legacy peer keeps its list-changed push. |
| `StreamableClientTransport.OAuthHandler` | OPT-OUT | Every mounted server is loopback or an Aura-controlled fork authenticated by `MCP_BEARER_TOKEN` / `MCP_HEADER_*` from the managed config. No OAuth flow exists to hand it. Re-open if a third-party remote server is ever mounted. |
| `SSEClientTransport` (`sse.go:356`) | OPT-OUT | Superseded by streamable HTTP; no mounted server speaks the legacy SSE transport. |
| `StdioTransport` / `IOTransport` (`transport.go:114`, `:130`) | OPT-OUT | Server-side / process-role transports. Aura is the client and spawns children via `CommandTransport`. |
| `InMemoryTransport` / `NewInMemoryTransports` (`transport.go:145`, `:160`) | INTEGRATE (test tier) | **Load-bearing.** A `net.Pipe()` pair yielding a **real** `*ClientSession` bound to a real in-process server — which is what makes dropping the `Server` interface cost nothing in testability, and what carries the 85% coverage floor after ≈900 LOC of transport tests are deleted. |
| `LoggingTransport` (`transport.go:313`) | OPT-OUT | A wire-dump debugging aid. `ClientOptions.Logger` plus Aura's `obs.Boundary` metrics cover the operational need without logging tool payloads (which carry memory content and PII). |
| `ClientOptions.Logger *slog.Logger` | INTEGRATE | The SDK's own observability seam. `observability.go` hangs off it rather than wrapping the client, so SDK-internal events (session teardown, ignored spec violations) land in Aura's log instead of vanishing. |

## Coverage summary

| | count |
|---|---|
| INTEGRATE | 22 |
| OPT-OUT (reasoned) | 22 |
| NOT IN SDK — Aura keeps it (stated, bounded) | 2 |

The two "Aura keeps it" rows are the phase's entire remaining bespoke surface: **redial policy**
and **tool-set-drift-marks-the-server-dead**. Both are host supervision decisions over a registry
the SDK cannot see. Neither contains JSON-RPC framing, a session model, or wire handling. If a
later reader finds a third, it is a regression against this table.
