# Pitfalls Research

**Domain:** Rich agentic operator web cockpit (embedded Vite/React SPA) on top of a Go single-binary SSE backend (Aura)
**Researched:** 2026-06-15
**Confidence:** HIGH for backend-grounded claims (verified against real Aura source); MEDIUM for the web/SSE/CSRF/WebGL best-practice points (verified against current docs + multiple sources)

> Scope note: this milestone adds the FULL operator cockpit + the two cross-cutting backend gaps it depends on — **GAP-1** richer AG-UI typed-display protocol (the event spine) and **GAP-2** web auth (the AG-UI gateway is loopback-only by design today, amendment #35; auth is currently Out of Scope in PROJECT.md §Multi-user). Most pitfalls below are *security-load-bearing* or *cheap-now/expensive-later*. Phase names below are suggestions for the roadmapper; they cluster into roughly: **(A) serve/auth/transport foundation**, **(B) AG-UI typed-display protocol**, **(C) operator-OS shell + ui_control**, **(D) governance UI (MCP/skills/web-safety)**, **(E) graph explorer**, **(F) packaging/embed/observability/PWA**.

---

## Critical Pitfalls

### Pitfall 1: Treating SSE reconnect as "the browser handles it" — it does not for AG-UI

**What goes wrong:**
The cockpit assumes the browser's `EventSource` auto-reconnect + `Last-Event-ID` will resume a dropped stream. It won't. `@ag-ui/client`'s `HttpAgent` streams over **`fetch` POST + a ReadableStream reader**, not `EventSource`, because an AG-UI run requires a request body (`RunAgentInput`). The browser's automatic reconnect/`Last-Event-ID` machinery only applies to native `EventSource` GETs — so a dropped connection mid-run silently stops yielding events with **no** auto-reconnect and **no** resume, leaving a half-rendered timeline and an orphaned (or still-running) backend turn.

**Why it happens:**
Every "SSE 101" article describes `EventSource` + `Last-Event-ID`. The AG-UI transport looks like SSE on the wire (`text/event-stream`) so developers reach for the wrong mental model.

**How to avoid:**
- Design resume explicitly around the **two existing backend endpoints**: `POST /agent/run` (the live stream) and `GET /threads/{id}/messages` (the `MESSAGES_SNAPSHOT` one-shot, `internal/agui/server.go:handleMessages`). On reconnect, the client re-fetches the snapshot to rehydrate authoritative state, then decides whether to re-attach.
- Respect the backend's **single-writer-per-thread lock**: `handleRun` calls `TryLockThread` and returns **409 `ErrThreadBusy`** (`internal/agui/server.go:186-195`). A naive reconnect that re-POSTs `/agent/run` against a still-running turn will 409 — the UI must treat 409 as "run still in flight, poll snapshot / show running state," not as an error toast.
- The snapshot already projects a paused turn's `ToolCalls` (WR-04, `projectMessages`), so a reconnect onto a paused (ask_user) thread can rehydrate the pending approval. Verify the cockpit surfaces that, not an empty thread.
- Add client-side exponential backoff on the fetch-reader retry (the browser gives you none here).

**Warning signs:**
Timeline freezes after a network blip and never recovers; duplicated assistant turns after reconnect; 409s shown as hard errors; resume works in dev (localhost, no drops) but not on Wi-Fi/mobile.

**Phase to address:** (A) serve/auth/transport foundation — wire the reconnect+snapshot+409 contract before any rich rendering.

---

### Pitfall 2: Backpressure / slow-client drops are silent on the UI — dropped deltas look like a stalled agent

**What goes wrong:**
The backend SSE pump (`streamSSE` / `pumpSend` in `internal/agui/server.go`) deliberately **drops non-lifecycle delta frames** when the per-connection buffer (default cap 64, `AURA_AGUI_BUFFER_CAP`) is full, to never block the agent Loop (T-12-09). Lifecycle frames (RUN_STARTED/FINISHED/ERROR, TEXT/TOOL/REASONING start/end, CUSTOM, STATE_SNAPSHOT — see `isLifecycleFrame`) are protected, but text/reasoning *content deltas* can vanish. A cockpit that renders text purely by appending deltas will show **gaps or truncated answers** with no indication anything was dropped, while a slow browser tab (background, throttled, heavy graph render) makes this routine.

**Why it happens:**
The drop is a server-side correctness choice (protect the Loop). The UI never learns a delta was dropped — `recordSSEDropped()` only increments a server metric + WARN log.

**How to avoid:**
- On the protected `*_END` lifecycle frames, treat the **final text content as authoritative** and reconcile/replace the delta-accumulated buffer (the END frame carries or implies the complete message). Do not trust the delta concatenation as final.
- Surface a quiet "stream degraded / reconnect to refresh" affordance when the client detects a sequence gap (e.g., a TEXT_END without matching delta volume), and offer a snapshot re-fetch.
- Keep heavy work (graph layout, large display rendering) off the same task that drains the stream reader, so the *client* doesn't become the slow consumer that triggers drops.
- Consider raising `AURA_AGUI_BUFFER_CAP` for the cockpit connection, but treat that as a mitigation, not a fix — the gap-reconciliation is the fix.

**Warning signs:**
Answers occasionally end mid-sentence; reasoning panel shows partial thoughts; server logs `agui server: SSE client slow, dropping event`; `aura_sse_dropped` metric climbs under load.

**Phase to address:** (A) transport foundation (gap detection + END-frame reconciliation) and (B) AG-UI typed-display protocol (ensure END frames carry enough to reconcile).

---

### Pitfall 3: Web auth bolt-on quietly breaks the loopback "bind IS the control" model

**What goes wrong:**
Today the AG-UI gateway and setup wizard are **hardcoded loopback** (`AURA_AGUI_BIND` default `127.0.0.1:9080`, `AURA_SETUP_BIND` `127.0.0.1:9081`; `config.go:309/317`). The loopback bind is the *compensating control* for the auth-deferred posture (amendment #35; comment at `serve.go:218-221`: "the bind is hardcoded loopback ... the compensating control for the auth-deferred posture"). Adding the cockpit tempts a `--bind 0.0.0.0` so it's reachable from a phone — but if auth is added carelessly (or the bind is opened *before* auth lands), the daemon goes from "unreachable off-host" to "agent with shell_exec + filesystem + MCP mounts, exposed to the LAN."

**Why it happens:**
"Make it reachable from my phone" is the natural next step; the auth and the bind change are separate PRs and the bind one is trivial, so it ships first.

**How to avoid:**
- Treat GAP-2 (web auth) as a **hard prerequisite gate** for any non-loopback bind. Mirror the PRD's existing posture (`server.go:2796`): "`--bind` non-loopback richiede auth + fail-fast sotto local-only (#35)." Make a non-loopback bind **fail-fast at boot** unless auth is configured. This is cheap-now, catastrophic-later.
- Prefer **token/Authorization-header session** over cookies for the SPA (memory-held token), which sidesteps CSRF entirely for the API surface (see Pitfall 4). The existing setup wizard already uses a one-time `AURA_SETUP_TOKEN` printed to stdout (amendment #10) — reuse that bootstrap pattern to mint a cockpit session.
- Keep **liveness/readiness/metrics/debug endpoints** (`/healthz`, `/readyz`, `/metrics`, `/debug/vars` — all on `Mux()`) behind the same auth or on a separate loopback-only listener. `/debug/vars` (expvar) and `/metrics` leak operational shape; today they're safe only because of loopback.
- The agent's shell/filesystem/MCP power means the blast radius of an auth bypass is RCE-class. Threat-model it as such (golang-security STRIDE: Spoofing + Elevation of Privilege).

**Warning signs:**
A `--bind` flag or `AURA_AGUI_BIND=0.0.0.0` appears in a commit before an auth middleware exists; `/metrics` reachable without a token; "it works from my phone" demo predates the auth phase.

**Phase to address:** (A) serve/auth foundation — auth + bind-gating + endpoint protection land together, before the cockpit is reachable off-host.

---

### Pitfall 4: CSRF on state-changing endpoints — the agent can be driven by a forged cross-site request

**What goes wrong:**
`POST /agent/run` *starts an agentic run* (shell, filesystem, MCP, mail/WhatsApp sends). Other state-changers will follow (MCP enable/remove, skill approve, scheduler create/cancel, conversation delete). If the cockpit authenticates with a **cookie** and these endpoints accept simple/credentialed cross-origin requests, a malicious page the operator visits can forge a request that makes Aura *do things* — CSRF with an RCE-class blast radius. Note the current CORS knob (`AURA_AGUI_CORS_PERMISSIVE`) sets `Access-Control-Allow-Origin: *` (`withCORS`) — permissive CORS + cookie auth is the classic foot-gun combo.

**Why it happens:**
SSE/`fetch` POST feels "API-like" so CSRF is assumed handled; the permissive-CORS dev knob gets left on; cookie auth is the default reflex.

**How to avoid:**
- **Prefer Authorization-header (bearer) auth with the token in memory** — an attacker site cannot set a custom header cross-origin, so CSRF protection is structurally unnecessary for the API (OWASP / Clerk guidance). This is the cleanest fit for a same-binary SPA.
- If cookies are used anyway: set `SameSite=Strict` (or `Lax`), `HttpOnly`, `Secure`, **and** require a CSRF token header on every POST/PUT/PATCH/DELETE (`/agent/run`, MCP mutations, skill approve, scheduler, conversation delete). SameSite alone is not sufficient (PortSwigger: bypasses exist).
- **Never ship `AURA_AGUI_CORS_PERMISSIVE=1` with a non-loopback bind / cookie auth.** Make permissive CORS mutually exclusive with cookie auth at config-validation time.
- Reuse the same protection for the setup wizard's `/setup/*` state-changers.

**Warning signs:**
`Access-Control-Allow-Origin: *` in prod config; cookie auth with no CSRF token; state-changing GETs; a state-changing endpoint that works from a `curl` with only a cookie and no custom header.

**Phase to address:** (A) serve/auth foundation. Cheap-now (pick header auth), expensive-later (retrofitting CSRF tokens across every mutation).

---

### Pitfall 5: `ui_control` becomes arbitrary frontend automation (the design's biggest footgun)

**What goes wrong:**
The agent can emit `ui_control` events to drive the cockpit (`open_panel`, `highlight_source`, `set_mode`, `show_job`, `set_density`, `theme_preview`). If the frontend treats these as a generic "do what the model says" channel, an LLM (steered by a malicious web page it fetched, a poisoned MCP tool result, or prompt injection in a document) can pivot into **client-side automation**: navigating the operator, hiding warnings, faking approvals' visual context, injecting CSS/DOM, or exfiltrating via crafted targets. The ux-spec is explicit: "Do not let AI UI-control events become arbitrary frontend automation. Everything must be allowlisted, scoped, logged, and reversible."

**Why it happens:**
It's easy to write a generic dispatcher (`applyUiControl(event)`) and hard to resist adding "just one more" control. The model output is *untrusted input* but feels first-party because it's "our agent."

**How to avoid:**
- **Closed allowlist of verbs**, validated server-side AND client-side. Honor the ux-spec contract exactly: `open_panel` → panel id from an allowlist only; `set_mode` → one of `chat|tree|graph|displays|settings`; `highlight_source` → an owned, DOM-safe internal id; `show_job` → a job id **owned by the active run/user**; `set_density` → `compact|operator|review`; `theme_preview` → a token object **validated by schema, no arbitrary CSS**.
- **Reject, never coerce**, anything off-allowlist. "The model must never emit raw CSS selectors, scripts, URLs to execute, or unbounded DOM mutations" (ux-spec). Drop + audit unknown verbs/targets.
- **Scope every target to the current run/user** so one run cannot drive another's UI or reference cross-run job ids.
- **Audit + replay**: "UI-control events should be replayable from the run log so debugging a session reconstructs what the operator saw." Persist each accepted/rejected ui_control with the run id. This is the only way to forensically answer "what did the agent make the screen do?"
- ui_control must be **cosmetic only** — it can *suggest* and *navigate*, never *act*. An approval, a mount, a delete must always go through the explicit governance gate, never through a ui_control side effect.

**Warning signs:**
A `default:` branch in the ui_control handler that does anything but drop+log; `theme_preview` accepting a string of CSS; `highlight_source` taking a CSS selector or URL; no run-log row per ui_control; the model able to switch modes into `settings` and trigger a config change in one flow.

**Phase to address:** (C) operator-OS shell + ui_control — build the allowlist/scope/audit/replay as the *first* thing in that phase, not after the verbs work.

---

### Pitfall 6: Rendering a skill/MCP secret, or showing pending state wrong

**What goes wrong:**
Three distinct leaks, all security-load-bearing:
1. **Rendering a saved MCP secret.** Env values "are editable but never displayed raw after save" (ux-spec); "Do not show raw saved MCP secrets in the UI." The backend stores env as `KEY=VALUE` strings in `~/.aura/mcp/servers.json` (0600) and only ever *redacts* for display (`mcp.RedactSecrets`, `manager.authStatus` infers posture without echoing the value). A cockpit that GETs the raw config to populate an "edit env" form will ship the plaintext token to the browser.
2. **Surfacing an internal error that embeds a secret.** Tool/infra errors can carry DSNs/bearer tokens; the backend sanitizes at the wire (`SanitizeString` / `redactEvent` in `agui/server.go`, `mcp.RedactSecrets`). If the cockpit adds a *new* error surface (a config-validation endpoint, an MCP doctor stderr panel) it must route through the same sanitizer — a new endpoint is a new leak path.
3. **Redacted-state correctness.** The UI must distinguish required / optional / missing / **redacted-but-set** states (ux-spec) and warn when "required recipe env variables still contain placeholders" (the backend's `authStatus` already detects `CHANGE_ME` / `${...}` placeholders). Showing "redacted" for an *empty* required var hides a misconfiguration.

**Why it happens:**
The "edit env" UX naturally wants to prefill the field; error panels naturally want the full error; redaction state is a 4-way enum that's easy to collapse to a boolean.

**How to avoid:**
- **Never return raw env values to the browser.** The edit flow sends *new* values down; it never receives saved ones. Display only redacted chips + a per-key state (required/optional/missing/redacted-set/placeholder).
- Route **every** new error/log/doctor surface through `SanitizeString` / `mcp.RedactSecrets` server-side *before* it reaches the response body. Add a test asserting a known token shape never appears in any cockpit response.
- Model the env-key state as the explicit 4–5-way enum from the ux-spec, driven by the backend's `authStatus`-style detection, so "missing-required" is visually distinct from "set-and-redacted."

**Warning signs:**
A network response containing `SMTP_PASS=...` or a bearer token; an edit form prefilled with a real secret; a doctor/stderr panel showing a full DSN; "redacted" rendered for a never-set required var.

**Phase to address:** (D) governance UI (MCP + skills). Highest-priority security item in that phase.

---

### Pitfall 7: Pending skills running, injecting prompt content, or the UI lying about activation

**What goes wrong + a real backend/spec contradiction to resolve:**
The ux-spec's non-goals say: "Do not activate installed or generated skills directly from a model tool call" and "Do not allow pending skills to run, inject prompt content, or override active skills before approval." **But the shipped backend does NOT match this for the in-box path.** Per the live tool schema (`skillParamsSchemaHonest` in `internal/agent/tools/skill.go`) and `writeAction` (`skill_write.go:164-174`, P5 2026-06-10): *model-authored `create`/`update` with `always:false` activate IMMEDIATELY in this container after validation + audit* (container = boundary, Claude-Code parity). Only `always:true` create/update and `delete` are approval-gated and staged pending. `save_snippet` stages pending. So:
- If the cockpit renders the **aspirational** non-goal ("nothing activates without approval"), it will **lie** — model-authored skills are already live, and the operator won't see an approval prompt that the backend no longer issues.
- Conversely, for the genuinely gated actions (`always:true`, `delete`, snippet save), a UI that lets the operator "run" or "preview-inject" a *pending* skill before approval breaks the real gate.

**Why it happens:**
The design doc predates / diverges from the P5 in-box-activation decision. Two sources of truth disagree; the UI is built from the doc.

**How to avoid:**
- **Make the UI mirror the *backend's actual* state machine, not the doc.** Surface the real distinction: `always:false` model-authored create/update = *active immediately (audited, in-box)*; `always:true` + `delete` + `save_snippet` = *pending → approval-gated*. Show the audit row for the auto-activated ones so the operator still *sees* the self-extension (the headless `Alerter` path exists for exactly this, `skill_write.go`).
- For the gated path, enforce the non-goal rigorously: **pending skills (status `pending_approval`, living under `~/.aura/skills/pending/<name>/`) must never be runnable, previewable-as-injected, or shown as active** in the library. The active/pending/archived/audit tabs (ux-spec Frame 08) must be *backed by real status*, not optimistic UI.
- **No model-facing approve.** Activation is human-only via `ask_user` resume or `aura skills approve` CLI (D-03, `skill.go:182-185`). The cockpit's approve action must hit the resume/CLI path, not a tool call.
- **Flag the doc/backend divergence to the roadmapper** as a decision to settle: either ship the UI honest to P5, or re-gate in-box activation (a backend change). Do not paper over it in the UI.

**Warning signs:**
An approval queue that never receives `always:false` create/update events (because they don't pause); a "run pending skill" button; the library showing a pending skill as runnable; UI copy promising "nothing runs without approval" while skills self-activate in chat.

**Phase to address:** (D) governance UI (skills). Resolve the doc/backend contradiction *during discuss-phase*, before building the queue.

---

### Pitfall 8: SSRF / web-safety states leaking internal detail, or mis-rendering the error enum

**What goes wrong:**
The backend is careful: SSRF block reasons name a *class* never a concrete IP/host/CIDR (`ReasonPrivateOrMetadata = "private_or_metadata_target"`, not `169.254.169.254`; `internal/web/errors.go`), and the rich `internalError` (with `resolvedIP`/`host`/`redirectFrom`) is **never** sent to the model — `sanitize()` is the single chokepoint. The cockpit can undo this in two ways: (1) by fetching/rendering a *richer* internal error surface (a debug endpoint, raw stderr) that re-exposes the IP/host; (2) by treating the safe enum as free text and mangling it. The ux-spec is explicit: "SSRF blocks must show safe reasons only" and web-safety events are *typed displays*, not text appended to the answer.

**Why it happens:**
Debugging convenience ("why was it blocked? show me the IP") plus the temptation to render `web_safety_event.message` as a generic toast.

**How to avoid:**
- Render web-safety as a **typed `system_event` / `web_safety_event` display** keyed off the stable enum: `web_search_unavailable`, `blocked_url`, `unsupported_scheme`, `unsupported_content_type`, `response_too_large`, `timeout`, `http_error`, `extraction_failed`, plus reasons `searxng_not_configured` / `searxng_unreachable` / `private_or_metadata_target` / `redirect_to_blocked_target` / `invalid_target` (`internal/web/errors.go`). Map each to **fixed, safe remediation copy** — do not interpolate the raw `message` into an IP-revealing string.
- **Never add a cockpit endpoint that returns the `internalError`.** If a debug view is needed, it must go through `AsWebError`/`sanitize` like everything else. The sensitive fields are unexported precisely to prevent accidental marshalling — don't add a sibling exported one.
- Treat the enum as a **contract**: the comment "never rename without a PRD amendment" applies to the UI too — a switch on these strings with a safe `default:` (generic copy, never the raw message) avoids both leaks and blank cards on a new enum value.
- Do **not** expose raw SearXNG backend parameters to operator or model (ux-spec non-goal) — the search panel surfaces results, not query internals.

**Warning signs:**
A blocked-URL card showing `169.254.x.x` or an internal hostname; a redirect-block card showing the redirect target; rendering `message` verbatim; a new "web debug" endpoint returning host/IP.

**Phase to address:** (D) governance UI / web-safety states, with the enum contract pinned in (B) the typed-display protocol.

---

### Pitfall 9: Neo4j graph rendered as a hairball / non-deterministic / GPU-melting on the mini-PC

**What goes wrong:**
Dumping a Cypher result into a force-directed canvas produces a "hairball" that answers no question, re-lays-out differently every run (force layouts are non-deterministic), and on the shared mini-PC (no discrete GPU, WebGL software/iGPU fallback) tanks the whole host. Canvas/2D libs choke around ~5k nodes; WebGL gets to ~10k but the mini-PC won't have the GPU headroom, and a background browser tab throttles WebGL — making the graph the *slow consumer* that triggers SSE drops (Pitfall 2). The ux-spec is explicit: "Do not render Neo4j as a decorative hairball" and "Dense graphs should default to filtered evidence paths, not hairball views."

**Why it happens:**
"Just visualize the graph" with a default force layout and no node cap; testing on a dev laptop with a real GPU.

**How to avoid:**
- **Default to 20–80 visible nodes** of *selected evidence paths* (ux-spec rendering rule), collapse high-degree neighbors behind expandable clusters, expand only on intent.
- **Deterministic layout per query** ("repeated runs do not jump") — seed the layout / cache positions keyed by the query so a re-run is stable. Honor the graph payload contract (`nodes/edges/paths/schema/query` with stable ids) so positions can be keyed off stable node ids.
- **Always pair the canvas with a readable textual path + inspector**, and **offer a table fallback** for accessibility, export, and very large result sets (ux-spec). The table fallback is the escape hatch when WebGL is unavailable/too slow.
- **Budget for the mini-PC** (memory `feedback_minipc_cpu_budget`, `feedback_gpu_not_for_embedding_workload`): cap node count, debounce layout, stop the simulation once settled (don't run the force tick forever), and never run layout on the stream-draining task.
- Hover cannot be the only access path — tap/focus opens the inspector on mobile/keyboard (ux-spec Frame 06).

**Warning signs:**
Graph layout visibly different on each run; node count unbounded; the tab pegs a CPU core while the graph is open; SSE drops spike when the graph view is active; no table fallback; mobile users can't open the node inspector.

**Phase to address:** (E) Neo4j graph explorer. Node-cap + deterministic layout + table fallback are acceptance criteria, not polish.

---

### Pitfall 10: Stale embedded assets / broken dev-vs-prod serving / Node toolchain in the image

**What goes wrong:**
Single-binary `//go:embed` of the Vite `dist/` has a well-known failure cluster:
- **Stale assets:** `go build` embeds whatever is in `dist/` *at compile time*. If the Vite build doesn't run before `go build` (CI ordering, local muscle memory), the binary ships the *previous* frontend — silently. "Works in dev, old UI in the binary."
- **`embed` of a missing/empty dir** fails the Go build (or embeds nothing), and an empty `dist/` is easy to produce.
- **SPA deep-link 404s:** embedded static serving must fall back unknown paths to `index.html` or client-side routes 404 on refresh.
- **Dev vs embedded:** the same handler must proxy to the Vite dev server in dev (HMR) and serve embedded `dist/` in prod, gated by an env/build tag — getting this wrong means either no HMR in dev or a proxy attempt in prod.
- **Docker image bloat / supply chain:** putting the full Node toolchain in the runtime image (instead of a multi-stage build that builds the SPA then copies only the binary) bloats the image and widens the attack surface; the existing Dockerfile is offline-first (vendored MCP bins, pinned commits — `catalog.go`).

**Why it happens:**
Build ordering is implicit; the embed is "set once and forget"; the dev/prod switch is added late.

**How to avoid:**
- **Make the SPA build a hard dependency of `go build`** in the Makefile/CI: `vite build` → assert `dist/index.html` exists and is newer than sources → `go build`. Fail the build if `dist/` is empty/stale (mirror the existing `make quality` discipline). Consider a content-hash check so a stale embed fails CI.
- Use the proven dev/prod pattern: **build tag or `ENV=dev` proxy to Vite** in dev, `//go:embed dist` + `fs.Sub` in prod, with an **`index.html` SPA fallback** for unknown non-asset paths.
- **Multi-stage Docker:** Node stage builds the SPA, Go stage embeds + compiles, runtime stage carries only the binary + sidecars. No Node in the runtime image. Preserve the offline-first posture (`feedback_preserve_docker_build_cache` — never prune the ~45–60 min stack cache).
- Pin the Node/Vite version (Vite 8 / Node 24 per the milestone stack) in CI to avoid cross-platform lock drift (memory `feedback_npm_lock_cross_platform_drift`).

**Warning signs:**
The binary shows an old UI after a frontend change; `index.html: no such file` build errors; refresh on a deep link 404s; Node in the runtime image; CI builds Go before the frontend.

**Phase to address:** (F) packaging/embed foundation. Build-ordering guard is cheap-now, debugging "why is my UI old" is expensive-later.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Open `--bind 0.0.0.0` before web auth lands | Reachable from phone for a demo | LAN-exposed RCE-class agent (shell/fs/MCP) | **Never** — gate non-loopback bind on auth, fail-fast |
| Cookie auth without CSRF tokens | Familiar, "browser handles it" | Forged cross-site runs/mutations | Never for state-changers; prefer header auth instead |
| Generic `applyUiControl(event)` dispatcher | Ship ui_control fast | Arbitrary frontend automation, no audit/replay | Never — allowlist + scope + audit from day one |
| Render raw config (incl. env) into the edit form | One GET prefills everything | Plaintext secret shipped to browser | Never — send new values down only |
| Force layout with no node cap | "It visualizes the graph" | Hairball, non-deterministic, GPU melt on mini-PC | Never as default; allow opt-in expand |
| Skip the SPA-build-before-go-build guard | Faster local loop | Stale embedded UI ships silently | Only in throwaway local builds, never CI |
| Trust delta concatenation as final text | Simple append renderer | Truncated answers on slow-client drops | Never — reconcile on END frames |
| UI built from ux-spec non-goals as if they're backend truth | Matches the design doc | UI lies about skill activation (P5 divergence) | Never — mirror real backend state |
| Permissive CORS left on (`AURA_AGUI_CORS_PERMISSIVE=1`) | Cross-origin dev convenience | CSRF amplifier with cookie auth / open bind | Dev-only, loopback-only, mutually exclusive with cookie auth |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `@ag-ui/client` HttpAgent (SSE over fetch-POST) | Assuming `EventSource`/`Last-Event-ID` auto-reconnect | Explicit reconnect → `GET /threads/{id}/messages` snapshot; handle 409 `ErrThreadBusy`; client-side backoff |
| AG-UI SSE pump | Rendering only delta concatenation | Reconcile on protected lifecycle END frames; detect gaps; raise `AURA_AGUI_BUFFER_CAP` as mitigation only |
| Neo4j MCP graph result | Dump nodes/edges into force layout | Normalize to the `nodes/edges/paths/schema/query` contract; 20–80 node cap; deterministic layout; table fallback |
| MCP managed config (`servers.json`) | GET raw env to prefill edit form | Redacted chips + per-key state enum; never return saved values |
| MCP doctor / stderr panel | Render raw stderr | Route through `mcp.RedactSecrets` first |
| Web-safety / SSRF block | Show IP/host or render `message` verbatim | Switch on the stable enum → fixed safe remediation copy; never expose `internalError` |
| Skill governance | Build approval queue from ux-spec non-goals | Mirror real backend state machine (P5 in-box auto-activation vs gated `always:true`/`delete`/snippet) |
| Setup wizard (`:9081`) bootstrap | New ad-hoc cockpit auth | Reuse `AURA_SETUP_TOKEN` one-time-token bootstrap (amendment #10) |
| `/metrics` `/debug/vars` (expvar/prom) | Left open with the open bind | Same auth or separate loopback-only listener |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Graph force layout uncapped | CPU core pegged, hairball, non-deterministic | 20–80 node cap, stop sim when settled, deterministic seed | >~2–5k nodes (Canvas) / GPU-starved mini-PC at far fewer |
| Heavy render on the stream-draining task | SSE deltas dropped (Pitfall 2) | Render off the reader task; reconcile on END | Whenever graph/large display renders while streaming |
| Metric cardinality explosion | Prometheus memory growth, slow `/metrics` | Low-cardinality labels: never per-conversation/per-URL/per-tool-call-id as a label; use IDs in structured logs, not metric labels (golang-error-handling rule 15) | Grows unbounded with conversations/runs |
| Unbounded SSE connections | Goroutine/heap growth | Cap concurrent connections; the pump is goleak-clean on ctx-cancel — keep it that way | Many tabs / reconnect storms |
| Background-tab WebGL throttling | Graph stalls, becomes slow consumer | Pause simulation when tab hidden (Page Visibility) | Operator backgrounds the cockpit |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Non-loopback bind without auth | LAN-exposed RCE-class agent | Fail-fast boot unless auth configured (amendment #35 posture) |
| Cookie auth + permissive CORS, no CSRF token | Forged agent runs/mutations | Header (bearer) auth in memory, or SameSite+HttpOnly+Secure+CSRF token on all mutations |
| Returning raw MCP env / config to browser | Secret exfiltration | Never return saved secrets; redacted chips only |
| New error/log/doctor surface bypassing sanitizers | DSN/token/IP/host leak | Route through `SanitizeString` / `mcp.RedactSecrets` / `AsWebError`; test no token in any response |
| `ui_control` without allowlist/scope | Arbitrary frontend automation, social-engineering the operator | Closed allowlist, run/user-scoped targets, reject+audit unknowns, replayable log |
| Pending skill runnable/injectable | Unapproved self-extension takes effect | Pending status hard-blocks run/inject/active; approve only via resume/CLI |
| Rendering model output as HTML | XSS from poisoned tool results / injection | Auto-escape (React does by default); never `dangerouslySetInnerHTML` on model/tool/web content without sanitization |
| `/metrics` `/debug/vars` exposed | Operational shape / internal detail leak | Auth-gate or loopback-only |
| SSRF reason exposing IP/host | Internal topology disclosure | Stable class enum only (`private_or_metadata_target`), never concrete address |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Theme/density boot flash (FOUC) | Light flash then dark; layout jump | Apply persisted theme/density **before app boot** (ux-spec Frame 07); inline pre-paint script |
| Flattening typed payloads into generic cards | Loses the whole point of the cockpit | Typed display router (`web_result`/`document`/`code`/`graph_*`/`swarm_report`/`system_event`...) per ux-spec |
| Dropped SSE deltas shown as a stalled agent | Operator thinks Aura hung | Surface "stream degraded — refresh" + snapshot re-fetch |
| Dock/window state lost on minimize | Re-do research/compare from scratch | Dock chips preserve progress/selection/streams (ux-spec Frame 07) |
| ui_control steals active selection | Operator's chat/graph/source selection yanked away | Tool state must not steal active selection (ux-spec Design Direction) |
| Hover-only graph inspector | Mobile/keyboard users locked out | Tap/focus opens inspector drawer |
| Missing PWA install metadata | Can't install to home screen; wrong theme-color | Provide manifest + theme-color; paint before boot |
| Approval queue weaker on mobile | Operators approve risky actions with less context | "Mobile uses the same approval queue as a drawer, not a reduced-risk shortcut" (ux-spec) |

## "Looks Done But Isn't" Checklist

- [ ] **SSE streaming:** Often missing reconnect/resume — verify a mid-run network drop rehydrates from `GET /threads/{id}/messages` and handles 409 `ErrThreadBusy` (not just "works on localhost").
- [ ] **SSE rendering:** Often missing drop-resilience — verify a slow client (throttled tab) still shows the *complete* final answer via END-frame reconciliation, not truncated deltas.
- [ ] **Web auth:** Often missing bind-gating — verify a non-loopback bind **fails fast** without auth, and `/metrics` `/debug/vars` are protected.
- [ ] **CSRF:** Often missing on state-changers — verify every POST/mutation rejects a request lacking the custom header/CSRF token (test with cookie-only `curl`).
- [ ] **ui_control:** Often missing the deny path — verify an off-allowlist verb/target is dropped + audited, and every accepted event has a replayable run-log row.
- [ ] **MCP env:** Often missing redaction on edit — verify no network response ever contains a saved secret; verify the 4–5-way key-state enum (incl. placeholder detection).
- [ ] **Skills governance:** Often missing real-state backing — verify the UI matches the *backend* (P5 in-box auto-activation surfaced via audit; gated path truly blocks pending run/inject).
- [ ] **Web-safety:** Often missing the safe-copy contract — verify no blocked-URL/redirect card reveals an IP/host; verify a new enum value falls back to safe generic copy, not a blank card.
- [ ] **Graph:** Often missing the table fallback + node cap — verify 20–80 default, deterministic re-run, table fallback works with WebGL disabled.
- [ ] **Embed:** Often missing the build-order guard — verify a frontend change without a frontend rebuild **fails CI** rather than shipping a stale UI.
- [ ] **Observability:** Often missing low-cardinality discipline — verify no per-conversation/per-URL metric labels; verify no secret in logs/traces.
- [ ] **PWA/theme:** Often missing pre-boot paint — verify no FOUC; verify install metadata + theme-color.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Non-loopback bind shipped without auth | HIGH | Immediately revert to loopback; rotate any exposed secrets/tokens; add bind-gating before re-exposing |
| Secret rendered to browser | HIGH | Rotate the leaked credential; remove the raw-config endpoint; add a no-token-in-response test |
| ui_control was a generic dispatcher | MEDIUM | Replace with allowlist; add audit/replay; review run logs for abuse during the window |
| Stale embedded UI shipped | LOW | Add the build-order guard; rebuild; (no security impact, just confusion) |
| Truncated answers from SSE drops | MEDIUM | Add END-frame reconciliation + gap detection; raise buffer cap as stopgap |
| Graph hairball / GPU melt | LOW–MEDIUM | Add node cap + deterministic layout + table fallback; pause sim on hidden tab |
| UI lied about skill activation (P5) | MEDIUM | Re-source the skills UI from backend status; settle the doc/backend divergence (PRD amendment) |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1. SSE reconnect/resume (no EventSource) | (A) serve/auth/transport foundation | Mid-run drop test rehydrates via snapshot; 409 handled |
| 2. Backpressure / slow-client drops | (A) transport + (B) typed-display protocol | Throttled-client test yields complete final answer |
| 3. Auth breaks loopback model | (A) serve/auth foundation | Non-loopback bind fails fast without auth; endpoints gated |
| 4. CSRF on state-changers | (A) serve/auth foundation | Cookie-only request to a mutation is rejected |
| 5. ui_control = arbitrary automation | (C) operator-OS shell + ui_control | Off-allowlist verb dropped+audited; replay reconstructs session |
| 6. Secret leakage in UI | (D) governance UI (MCP/skills) | No secret in any response; key-state enum correct |
| 7. Pending-skill / activation correctness | (D) governance UI (skills) | UI matches backend state machine; pending blocked |
| 8. SSRF/web-safety leak + enum render | (B) typed-display + (D) web-safety states | No IP/host in cards; safe-copy default branch |
| 9. Graph hairball / mini-PC GPU | (E) Neo4j graph explorer | 20–80 cap, deterministic, table fallback w/ WebGL off |
| 10. Stale embed / build order / Node image | (F) packaging/embed foundation | Frontend change w/o rebuild fails CI; no Node in runtime image |
| Metric cardinality / log-secret | (F) observability | No high-cardinality labels; no secret in logs/traces |
| Theme boot flash / PWA | (F) PWA polish | No FOUC; install metadata present |
| The 13 ux-spec non-goals | enforced across (B)(C)(D)(E) | Each non-goal has a guard/test (see below) |

## The ux-spec's 13 Non-Goals as Enforceable Guards

The design lists explicit non-goals; treat each as a testable guard the roadmap must enforce:

1. Don't copy Elysia's Weaviate collection model → no collection abstraction in the source explorer.
2. Don't use the abstract sphere as main decoration → trust shown via source/provenance/execution structure.
3. No swarm talk/join/mailbox/sibling-chat UI → swarm = worker *report* table only (ux-spec Frame 02: not a fake inter-agent chat graph).
4. Don't expose raw SearXNG params to operator/model → search panel shows results, not query internals.
5. Don't flatten every payload into generic cards → typed display router is mandatory.
6. Don't render Neo4j as a hairball → **Pitfall 9** (evidence paths, provenance visible).
7. Don't copy Odysseus personal-workspace sprawl → dock/window only where it improves investigation.
8. Don't let ui_control become arbitrary automation → **Pitfall 5** (allowlist/scope/log/reversible).
9. Don't show raw saved MCP secrets → **Pitfall 6**.
10. Don't silently mount destructive MCP tools when an allowlist exists → denied/destructive tools shown explicitly (mail/WhatsApp), backed by real trust/policy (`NormalizedTrust`, `TrustBlocked`).
11. Don't activate installed/generated skills directly from a model tool call → **Pitfall 7** (note P5 divergence to resolve).
12. Don't present skills install as safe because `--ignore-scripts` → keep RISKY supply-chain framing in the install checklist.
13. Don't let pending skills run/inject/override before approval → **Pitfall 7** (status hard-blocks).

## Sources

- Real Aura backend (verified, HIGH confidence): `internal/agui/server.go` (SSE pump, slow-client drop, lifecycle-frame protection, `TryLockThread`/409, `SanitizeString`/`redactEvent`, CORS knob), `internal/web/errors.go` + `internal/web/ssrf.go` (stable safe enum, class-not-IP reasons, `internalError` never crosses model boundary), `internal/mcp/managed_config.go` + `internal/mcp/manager/{runtime,status,catalog}.go` + `internal/mcp/redact.go` (trust classes, `TrustBlocked` gate, 0600 config, redaction, placeholder detection), `internal/agent/tools/skill.go` + `skill_write.go` (P5 in-box auto-activation vs gated path, no model-facing approve), `internal/agent/tools/shell_approval.go` (destructive-command approval pattern), `internal/scoring/scoring.go` (Safe/Normal/Risky/Destructive tiers, `GateRecommended`), `cmd/aura/serve.go` + `internal/config/config.go` (loopback-only bind, amendment #35 comment, distinct setup port).
- Design docs: `docs/design/aura-deep-search-figma/ux-spec.md` (Important Non-Goals, ui_control contract, web-safety states, MCP/skills governance, graph rendering rules), `BACKEND_CAPABILITY_MAP.md`.
- PRD `prd.md` (amendment #35 auth/bind posture line 2796; SSRF CIDR catalog 1928–1929; setup-token amendment #10 line 2928/5140).
- Aura memories: `feedback_minipc_cpu_budget`, `feedback_gpu_not_for_embedding_workload`, `feedback_preserve_docker_build_cache`, `feedback_npm_lock_cross_platform_drift`, `reference_mcp_sidecar_lifecycle_and_openclaw_host`.
- Web best-practice (MEDIUM, multi-source verified):
  - SSE / Last-Event-ID + the fetch-POST-not-EventSource transport: [MDN Using server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events), [HTML Living Standard §9.2](https://html.spec.whatwg.org/multipage/server-sent-events.html), [SSE over POST without EventSource](https://medium.com/@david.richards.tech/sse-server-sent-events-using-a-post-request-without-eventsource-1c0bd6f14425), [@ag-ui/client](https://www.npmjs.com/package/@ag-ui/client).
  - CSRF / SameSite / header-vs-cookie: [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html), [Clerk CSRF protection](https://clerk.com/docs/guides/secure/best-practices/csrf-protection), [PortSwigger SameSite bypasses](https://portswigger.net/web-security/csrf/bypassing-samesite-restrictions).
  - WebGL graph perf limits: [Cylynx JS graph viz comparison](https://www.cylynx.io/blog/a-comparison-of-javascript-graph-network-visualisation-libraries/), [Nightingale: visualizing a million-node graph](https://nightingaledvs.com/how-to-visualize-a-graph-with-a-million-nodes/), [reagraph](https://github.com/reaviz/reagraph).
  - Go embed + Vite single binary: [Embed Vite app in a Go Binary](https://www.tushar.ch/writing/embed-vite-app-in-go-binary), [Embed Vite React in Golang with live reload](https://dev.to/danhawkins/embed-vite-react-in-golang-binary-with-live-reload-1k4d).
- Skills: `golang-security` (STRIDE, header/cookie CSRF, binding to 0.0.0.0, returning detailed errors, timing/secret handling), `golang-concurrency` (goroutine exit/leak, ctx.Done in select, backpressure/buffer discipline — maps to the SSE pump), `golang-error-handling` (single-handling rule, low-cardinality log messages, never expose technical errors to users).

---
*Pitfalls research for: Aura operator web cockpit (frontend + infra milestone)*
*Researched: 2026-06-15*
