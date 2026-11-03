# Sign-in in a real browser (Slice 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A person connects any OAuth MCP server or a Google account from the cockpit, signing in inside a real desktop Chromium that runs in their own container, with no relay page in the path of a server that can register a loopback redirect.

**Architecture:** One container per sign-in, on the identity's egress floor, removed when the flow ends. A sidecar owns the netns, takes the loopback redirect and hands the query to Aura over an exec stream. The cockpit reaches the desktop through a reverse proxy that dials over a second exec stream, so nothing is published and the floor stays as it is. Every flow has one fallback, pasting the return address from the person's own browser.

**Tech Stack:** Go 1.27, moby client, sqlc and golang-migrate, React with tanstack query and react-i18next, `lscr.io/linuxserver/chromium`, C# for the PIM fork.

**Spec:** `docs/superpowers/specs/2026-11-02-sign-in-browser-design.md`, approved 2026-11-02. Executors read both.

## Global Constraints

- **Gates before code:** Task 1's S1 to S3 end in a dated file in `docs/superpowers/verification/`. A failed gate comes back to the spec before Tasks 5 to 7 start.
- **S2 amendment, 2026-11-02:** the operator approved the dedicated-origin fallback after the opaque sandbox failed. Native Selkies 2.1.0 requires client storage. S1–S3 passed in the combined Edge run. Tasks 5–7 below use that other origin with ownership checks and cross-origin write protection. Evidence: `docs/superpowers/verification/2026-11-02-sign-in-browser-spike.md`.
- **Migration number:** `ls internal/db/migrations/ | tail -1` at landing, then the next integer. 0150 was the last on 2026-11-02.
- **One new env var:** `AURA_SIGNIN_BROWSER_IMAGE`, pinned by digest, in `.env.example` and `compose.yaml`. The image is not in the appliance payload.
- **Payload:** a change to `compose.yaml` is committed first, then `make payload-manifest` in WSL, then the manifest in its own commit. The gate refuses a dirty payload file.
- **Files ≤ 600 LOC.** A file that would pass it splits on touch, into `<name>_<concern>.go`.
- **No model on this path.** No tool, prompt or agent route reaches a sign-in session.
- **Nothing gets a check passed:** no stealth flags, no solver, no change made to look less automated (spec §3).
- **Coverage:** every new package needs an entry in `scripts/coverage_package_policy.json`, and daemon-gated code needs daemon-free unit tests for its pure logic.
- **Database work** runs on a throwaway server from `scripts/lib/disposable_stack.sh`, with `CI=1` so a skip fails. A sub-second integration run is a skip.
- **The fork's `main` publishes an image,** so a push there waits for the operator to write «pubblica». Every push there is additive until Task 10, because an older Aura may run beside the new image.
- **The published site is outward-facing.** A change to `gitlab.com/Chetto1983/aura-connect` waits for the operator's yes.
- **The VM changes only through the updater.** Spikes run in throwaway containers and leave nothing behind.
- **Go runs in WSL.** Vitest and the bundle run on Windows, from `web/`, with `node node_modules/vitest/vitest.mjs run <file> --maxWorkers=4`.
- **Commits:** atomic, imperative, body says why, `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. A task closes on `master`.

## Review Focus

Each has its test in the task that owns the code.

1. **Another identity, or no session, on `/sign-in/<session>/`** must be refused before any exec runs. Task 6, `TestSignInProxyRefusesBeforeItDials`.
2. **The cockpit's cookie must never reach the container,** which runs a third party's code. Task 6, `TestSignInProxyNeverForwardsTheCockpitCookie`.
3. **A person who closes the tab** ends the flow and all three containers. Task 7, `TestClosingTheSignInPageEndsTheFlowAndItsContainers`.
4. **A pasted address that is wrong:** another flow's `state`, `error=access_denied`, a second paste, an expired flow, someone else's flow. Task 3, `TestPasteAddress`.
5. **Aura dies mid-flow,** leaving containers with a live desktop. Task 5, `TestSweepRemovesEverySignInContainerLeftBehind`.

## Decisions taken while writing this plan

- **The loopback redirect is `http://127.0.0.1:43121/aura/mcp/oauth/callback`,** not the `/callback` that was measured. The deployed `mcp/callback/relay.js` forwards only two exact paths, read 2026-11-02, and this is the one `aura mcp login` already uses. So a server with no registration keeps working through the relay, into the container's listener, with no change to the published site. `/callback` would have shown "Link not valid". Nothing else cares about the path: DCR registers what it is given.
- **The desktop uses the dedicated origin, approved after S2 on 2026-11-02.** Opaque subresources had no Strict cookie, and even diagnostic resource access exposed a fatal localStorage read in stock Selkies 2.1.0. A path credential cannot fix storage. Reuse Caddy's `:8444` keyhole and the existing sandbox-origin/port/bind configuration, on the cockpit's hostname so the cookie arrives. A different-hostname origin, plain LAN HTTP or a tunnel without the port takes the paste-back. The Caddy listener admits the stream only; the cockpit origin refuses its raw desktop path.
- **The desktop page sits in an `<iframe sandbox="allow-scripts allow-same-origin">` on a cockpit page,** `/signing-in/:session`, pointing to the dedicated origin, with `allow="clipboard-read; clipboard-write; autoplay"`. The cockpit page polls the flow and closes the tab. It cannot be `/sign-in/:session`: `ServeMux` answers that path by redirecting it into the proxy's subtree. Never grant this sandbox combination to third-party HTML on the cockpit origin.
- **A second port still receives same-site cookies.** The cockpit uses `http.CrossOriginProtection` on browser writes; no mutating handler may run for a simple POST or form submission from the desktop origin. Native frame-ancestor/WebSocket-origin settings are used as well, and the proxy strips cockpit credentials and enforces the final response CSP, including for Nginx-served static HTML.
- **The sidecar owns the netns, and starts first.** The floor comes up on it, and only then the desktop joins with the consent URL in `CHROME_CLI`, so Chromium never runs without the floor. The existing boxes start before their floor, which is harmless there because nothing runs in them yet.
- **A sign-in refuses to start with no floor.** `launchEgress` is a no-op when no egress image is configured, which is fine for a box and wrong here.
- **`Navigate` creates the containers.** `CHROME_CLI` needs the consent URL when the desktop is created, and the redirect is known before anything exists. So `Open` takes the redirect and allocates the session, and `Navigate` starts the three containers.
- **The stream's relay is Node, in the callback sidecar,** which shares the netns. `socat` is not in the sandbox image, and the sidecar has Node. `127.0.0.1:3000` answers there too, so the desktop needs no exec at all.
- **The image pull is its own step.** It took 225 s on the VM, past the 45 s startup timeout, so the card pulls first and starts the flow when the image is there.
- **Free disk is read with `df` in the identity's box,** because its overlay sits on Docker's disk, and Docker's API reports what Docker holds, not what the disk has left.
- **The state machine's tests move with it.** They read `b.pending` and `b.timeToLive`, so they cannot stay in `chatgptplan`. Their assertions do not change, only their setup, and the commit body says so. `internal/chatgptplan`'s service tests stay where they are.
- **`StartLoopbackAuthorization` is a new method.** The old `StartAuthorization` keeps Linear and Notion working until Task 7 removes it, so `master` is never without a way in.
- **A dead grant is a column,** `refused_at`, not a deleted row: the card says the access expired instead of looking never connected.
- **The Google client travels with the sign-in.** `StartGoogleOAuth` takes the client in its body and keeps it with the pending state, and Aura rewrites the account after the exchange succeeds. Rewriting before the consent would break a healthy account when the person then cancels.
- **The PIM gets a new exchange route.** Its callback answers a browser with an HTML page, and Aura needs a typed result. The spec's "existing callback" is corrected.
- **The relay goes last,** as spec §7 says, once the VM has run a full Google sign-in. That is its own task, and a second «pubblica».

## File structure

| Unit | Responsibility |
|---|---|
| `internal/browserlogin` | The sign-in state machine: session, TTL, one flow per owner. Knows no provider |
| `internal/sandbox/usersandbox/signin_box*.go` | Three containers for one sign-in, their limits, their sweep. No way to express host exposure |
| `internal/sandbox/usersandbox/exec_conn.go` | A `net.Conn` over an exec stream |
| `cmd/aura/serve_signin_browser.go` | `browserlogin.Launcher` over the backend, listener and disk check |
| `cmd/aura/callback-listener.mjs` | The loopback listener, shared with `chatgpt-browser.mjs` |
| `cmd/aura/signin-relay.mjs` | Pipes stdin and stdout to `127.0.0.1:3000` |
| `internal/agui/signin_proxy.go` | Refuses, then proxies `/sign-in/<session>/` |
| `internal/mcp/oauth_paste.go` | Finishes a flow from a pasted address |
| `web/src/routes/SignInPage.tsx` | Hosts the sandboxed desktop, polls the flow, closes the tab |
| `web/src/governance/SignInImageGate.tsx` | First-use download, shared by the MCP and PIM cards |

---

### Task 1: Spike S1 to S3, throwaway

Needs the operator at the mouse. No product code, nothing merged but the verification file.

**Files:**
- Create in the scratchpad, not the repo: `signin-spike/main.go`, `signin-spike/run-vm.sh`
- Create: `docs/superpowers/verification/<date>-sign-in-browser-spike.md`

- [x] **Step 1: Build the rig.** On the VM, in this order: a sandbox-image sidecar that owns the netns; `aura-egress` on it, applying the real floor from `buildFloorRuleset`; then `lscr.io/linuxserver/chromium` by digest on the same netns, with both harden switches, `SELKIES_FILE_TRANSFERS=none`, `SELKIES_ENABLE_SHARING=false`, `SUBFOLDER=/sign-in/spike/`. Note the host config it needed: the earlier run used Docker's defaults with `--shm-size=1gb`. The Go program serves HTTPS with a self-signed certificate on the VM's loopback, tunnelled to the PC. It reverse-proxies `/sign-in/spike/` over `docker exec -i <sidecar> node relay.mjs`. The initial opaque iframe failed; the approved combined run framed the dedicated origin with `sandbox="allow-scripts allow-same-origin"` and explicit clipboard permissions.
- [x] **Step 2: S1.** Run Canva's DCR and `/authorize` with `http://127.0.0.1:43121/aura/mcp/oauth/callback`, from a listener in the sidecar. Expected: token 200 and `tools/list` answering, under the floor.
- [x] **Step 3: S2.** The operator signs in through the proxy. Record picture, mouse, keyboard, paste, five minutes with no drop, and the page's load time, since each connection is one exec. From the iframe's console, `fetch('/api/governance/mcp')` must fail. Record whether the session cookie reached subresources and the websocket, and what `Origin` the upgrade carried.
- [x] **Step 4: S3.** With both switches on, try sudo, a terminal, a file transfer and right-click. Expected: sudo, terminals, file transfers and window-manager launch menus unavailable, with the sign-in of Step 3 still passing. Ordinary Chromium page menus are outside the native window-manager hardening.
- [x] **Step 5: Write the file and remove the rig.** Say what passed, what it does not prove, and which branch of §4.3 stands. Containers, volumes and the image leave the VM. Pin the digest used.
- [x] **Step 6: Amend what the measure contradicts,** in the spec with date and evidence, and in this plan's Tasks 5 to 7. Commit.

```bash
git add docs/superpowers/verification docs/superpowers/specs docs/superpowers/plans
git commit -m "docs(sign-in): record what the spike measured"
```

### Task 2: A dead grant reads as dead

A bug today, and independent of the rest.

**Files:**
- Create: `internal/db/migrations/<next>_mcp_oauth_refused.{up,down}.sql`
- Modify: `internal/db/queries/identity_mcp_oauth.sql`, `internal/mcpoauth/store.go`, `internal/mcp/oauth_tokensource.go`, `internal/mcp/oauth_handler.go`, `internal/agui/mcp_oauth_api.go`, `cmd/aura/mcp_oauth_provider.go`, `web/src/governance/McpAuthorizationPanel.tsx`, `web/src/governance/mcpAuthApi.ts`, `web/src/i18n/resources.governance.ts`
- Test: `internal/mcp/oauth_tokensource_test.go`, `internal/mcpoauth/store_integration_test.go`, `cmd/aura/mcp_oauth_provider_test.go`, `web/src/governance/__tests__/McpAuthorizationPanel.test.tsx`

**Interfaces:**
- Produces: `mcpoauth.Grant.RefusedAt time.Time`; `(*mcpoauth.Store).MarkRefused(ctx, serverName string) error`; `GrantStore.MarkRefused`; `agui.MCPAuthorization.Expired bool` as JSON `expired,omitempty`.

- [x] **Step 1: Write the failing tests.**

```go
func TestARefreshAnsweredInvalidGrantMarksTheGrantRefused(t *testing.T)
// source fails with &oauth2.RetrieveError{ErrorCode: "invalid_grant"}: store.refused == [name], Token() returns the error.
func TestATransientRefreshFailureLeavesTheGrantAlone(t *testing.T)
// a 503 RetrieveError and a plain network error: store.refused is empty.
func TestAStoredConsentClearsARefusal(t *testing.T) // db_integration: MarkRefused, then Save: Load gives a zero RefusedAt.
func TestAuthorizationStateSaysARefusedGrantExpired(t *testing.T)
// AuthorizationState = {Supported: true, Authorized: false, Expired: true}.
```

```tsx
it('offers to authorize again when the access expired', ...);
// state {supported: true, authorized: false, expired: true}: the expired text and the authorize button show.
```

- [x] **Step 2: Run them.** `go test ./internal/mcp/ ./cmd/aura/`. Expected: FAIL, `MarkRefused` undefined.
- [x] **Step 3: Implement.** Migration: `ALTER TABLE aura.identity_mcp_oauth ADD COLUMN refused_at timestamptz`. Query `MarkIdentityMCPOAuthRefused :exec`, and `refused_at = NULL` in the upsert's `DO UPDATE`. Run `make sqlc`. In `persistingTokenSource.Token`, match with `errors.AsType[*oauth2.RetrieveError]`, as `cmd/aura/serve_pimwatch.go` does. A failed `MarkRefused` is logged and the token error stays what is returned. Texts, en and it: `governance.mcp.detail.authorization.expired`, "Access expired: authorize again" and "Accesso scaduto: autorizza di nuovo".
- [x] **Step 4: Run all tiers.** `go test -race ./internal/mcp/ ./internal/mcpoauth/ ./cmd/aura/`, then `-tags=db_integration ./internal/mcpoauth/` on a throwaway server, then Vitest on the panel. Expected: PASS, and the integration run over a second.
- [x] **Step 5: Commit.** `fix(mcp): say so when a stored grant was refused`.

### Task 3: Paste the return address

Works with no container, and is Canva's first way in.

**Files:**
- Modify: `internal/mcp/oauth_flows.go` (split on touch if it passes 600 LOC), `internal/mcp/oauth_config.go`, `internal/agui/mcp_oauth_api.go`, `cmd/aura/mcp_oauth_provider.go`, `cmd/aura/mcp_login.go`, `cmd/aura/serve_webui.go`, `web/src/governance/McpAuthorizationPanel.tsx`, `web/src/governance/mcpAuthApi.ts`, `web/src/i18n/resources.governance.ts`
- Create: `internal/mcp/oauth_paste.go`, `internal/mcp/oauth_paste_test.go`, `internal/agui/mcp_oauth_paste_test.go`

**Interfaces:**
- Produces:
  - `const mcp.LoopbackCallbackPath = "/aura/mcp/oauth/callback"`, which replaces `mcpOAuthCallbackPath` in `cmd/aura/mcp_login.go`, and `const mcp.LoopbackCallback = "http://127.0.0.1:43121" + LoopbackCallbackPath`
  - `func (s OAuthSettings) SignInRedirect() string`: `MCP_OAUTH_REDIRECT_URL` when set, else `LoopbackCallback`
  - `func (s OAuthSettings) RedirectIsLoopback() bool`
  - `func (f *Flows) Paste(owner, id, address string) (Flow, error)`, with `ErrPasteMismatch`
  - `func (f *Flows) Cancel(owner, id string) error`, used by Task 7
  - `MCPAuthorizationProvider.StartLoopbackAuthorization(ctx, owner, server string) (mcp.Flow, error)` and `.PasteAuthorization(owner, id, address string) (mcp.Flow, error)`
  - Route `POST /api/governance/mcp/authorization/flow/{id}/paste`, body `{"address": "..."}`, under `governance.write`
  - `POST /api/governance/mcp/{name}/authorization` takes `{"via": "paste"}`

- [x] **Step 1: Write the failing test.**

```go
func TestPasteAddress(t *testing.T) // table on a flow waiting with state "s1":
// mcp.LoopbackCallback+"?state=s1&code=c"                 -> completes the flow
// "...?state=s2&code=c"                                   -> ErrPasteMismatch, the flow still waits
// "...?state=s1&error=access_denied&error_description=no" -> the flow fails, Error holds "access_denied"
// "not a url", "", 16 KiB + 1                             -> ErrPasteMismatch, the flow still waits
// the same good address twice                             -> second gives ErrFlowNotWaiting
// another owner's id                                      -> ErrNoSuchFlow, and no state compared
// an expired flow                                         -> ErrNoSuchFlow
func TestPasteTakesAnyHostBecauseOnlyTheStateProvesIt(t *testing.T) // https://example.com/cb?state=s1&code=c completes.
func TestPasteCarriesARelayedState(t *testing.T) // a flow published with "s1.<base64 target>" completes on that whole state.
func TestSignInRedirect(t *testing.T) // "" -> LoopbackCallback; "http://localhost:9000/cb" as given; "https://a.example/cb" as given, RedirectIsLoopback false.
func TestMCPPasteRoute(t *testing.T) // 200 flow; 409 on ErrPasteMismatch; 404 on ErrNoSuchFlow; 401 with no identity; the body never echoes the address.
```

- [x] **Step 2: Run it.** `go test ./internal/mcp/ ./internal/agui/`. Expected: FAIL, undefined.
- [x] **Step 3: Implement.** Compare `state` with `subtle.ConstantTimeCompare`. Cap the address at 16 KiB. Then call the existing `Complete` and `Fail`. `Paste` never logs the address.
- [x] **Step 4: Cockpit.** The panel gets a secondary action, "Use your own browser": it starts with `via: "paste"`, opens `authorizationUrl`, and shows one field with text "When the page fails to load, copy its address here". A refused paste shows the reason. Vitest: `it('completes a flow from a pasted address')`, `it('shows why a pasted address was refused')`.
- [x] **Step 5: Run and gate.** `go test -race ./internal/mcp/ ./internal/agui/ ./cmd/aura/`, Vitest on the panel, `tsc --noEmit`, oxlint, knip.
- [x] **Step 6: Commit,** then the bundle apart. `feat(mcp): finish an authorization from a pasted address`, `build(web): embed the paste-back`.

### Task 4: The sign-in state machine, in its own package

**Files:**
- Create: `internal/browserlogin/login.go`, `internal/browserlogin/*_test.go` (moved from `internal/chatgptplan/browser_*_test.go`)
- Modify: `internal/chatgptplan/browser.go` (reduced to the adapter), `internal/chatgptplan/browser_service.go`, `cmd/aura/serve_chatgpt_browser.go`, `internal/agui/settings_chatgpt.go`, `scripts/coverage_package_policy.json`
- Create: `cmd/aura/callback-listener.mjs`, moved out of `chatgpt-browser.mjs`

**Interfaces:**
- Produces:

```go
package browserlogin

type Session interface {
	RedirectURI() string
	Navigate(ctx context.Context, authURL string) error
	Callback(ctx context.Context) (url.Values, error)
	Close() error
}
// redirect is "" when the session picks its own, as ChatGPT's script does.
type LaunchOptions struct { Redirect, CockpitOrigin, StreamOrigin string }
type Launcher interface{ Open(ctx context.Context, session string, opts LaunchOptions) (Session, error) }
type Authorizer interface {
	Start(ctx context.Context, owner, redirectURI string) (authURL string, err error)
	Callback(ctx context.Context, query url.Values) error
	Cancel(owner, authURL string)
	Fail(owner, authURL, message string)
}
type Flow struct{ Route, Status string }
type Options struct {
	Prefix, Route  string // "chatgpt-" and "/browser/", or "signin-" and "/signing-in/"
	Startup, TTL   time.Duration
	CheckOwner     func(ctx context.Context, owner string) error
	Messages       Messages
}
func New(root context.Context, launcher Launcher, opts Options) *Login
func (l *Login) Start(ctx context.Context, owner, attempt string, opts LaunchOptions, a Authorizer) (Flow, error)
func (l *Login) Owns(ctx context.Context, owner, session string) bool
func (l *Login) Cancel(ctx context.Context, owner, route string) error
func (l *Login) Close() error
```

- `chatgptplan.NewBrowserLogin` keeps its signature and wraps `Login`, so `serve_chatgpt_browser.go`'s callers change only their imports. Its adapter supplies empty `LaunchOptions`; ChatGPT continues choosing its own redirect. MCP and Google supply their loopback redirect and per-attempt cockpit/desktop origins explicitly, not through mutable process-global state.
- `callback-listener.mjs` exports `listen({ path, port, expectedState, emit })`. The ChatGPT script imports it, and both files are written to the box.

- [x] **Step 1: Move the tests first.** Move `browser_attempt_test.go`, `browser_cleanup_test.go`, `browser_commit_test.go`, `browser_failures_test.go` to the new package, with a fake `Authorizer` in place of the service. Assertions do not change. Run `go test ./internal/browserlogin/`. Expected: FAIL, no such package.
- [x] **Step 2: Move the code.** `go test -race ./internal/browserlogin/ ./internal/chatgptplan/ ./internal/agui/ ./cmd/aura/`. Expected: PASS.
- [x] **Step 3: Two prefixes do not collide.** `TestTwoLoginsKeepTheirOwnSessions`: an owner with a `chatgpt-` and a `signin-` flow, where cancelling one leaves the other. `TestAnOwnerHasOneSignInAtATime`: a second `Start` with another attempt cancels the first. `TestStartHandsTheRedirectToTheLauncher`. `TestStartKeepsEachAttemptsOrigins`: each launcher receives only that attempt's two origins after replacement.
- [x] **Step 4: Prove ChatGPT still works.** `go test -tags=docker_integration -run ChatGPT ./cmd/aura/`, or the tier that holds its browser test, on the stack.
- [x] **Step 5: Coverage entry,** `"github.com/chetto1983/aura/internal/browserlogin": {"mode": "target"}`, and commit. `refactor(signin): lift the browser login out of chatgptplan`, with a body that says the tests moved because they read internals, and that no assertion changed.

### Task 5: The sign-in box

**Files:**
- Create: `internal/sandbox/usersandbox/signin_box.go`, `signin_box_spec.go`, `signin_box_test.go`, `signin_box_integration_test.go` (tag `docker_integration`), `cmd/aura/serve_signin_browser.go`, `cmd/aura/serve_signin_browser_test.go`
- Modify: `internal/sandbox/usersandbox/docker_backend.go` (extract `startEgress`), `internal/sandbox/usersandbox/docker_backend_reconcile.go`, `internal/config` (the image), `compose.yaml`, `.env.example`, `scripts/payload_manifest.txt`, `cmd/aura/serve_webui.go`

**Interfaces:**
- Consumes: `browserlogin.Launcher`, `browserlogin.Session`; `AdmitBrowser`; `buildEgressSidecar`; `mcp.LoopbackCallback`.
- Produces:

```go
type SignInSpec struct { // like SandboxSpec, it has no field for host network, binds, caps or the socket
	Session, IdentityID, DesktopImage, SidecarImage, AuthURL string
	CockpitOrigin, StreamOrigin string
	Egress EgressPolicy
}
func signInEnv(s SignInSpec) []string
func signInHostConfig(netnsOwner string) *container.HostConfig
func (b *DockerBackend) startEgress(ctx context.Context, name, netnsOwner string, policy EgressPolicy, labels map[string]string) error // launchEgress's body
func (b *DockerBackend) StartSignIn(ctx context.Context, s SignInSpec, prepare SignInPrepare) (SignInBox, error)
func (b *DockerBackend) StopSignIn(ctx context.Context, session string) error
func (b *DockerBackend) SweepSignIns(ctx context.Context) error
func (b *DockerBackend) SignInImageState(ctx context.Context, image string) (present bool, err error)
func (b *DockerBackend) PullSignInImage(ctx context.Context, image string, progress func(done, total int64)) error
type SignInBox struct{ Session string; Sidecar BoxHandle }
type SignInPrepare func(context.Context, SignInBox) error
const signInLabel = "aura.signin"
var ErrSignInNeedsTheFloor = errors.New("sandbox: a sign-in needs the egress floor")
```

Names: `aura-signin-<24 hex>`, with `-egress` and `-desktop`. Constants: `signInMemoryBytes = 1536 << 20`, `signInPidsLimit = 2048`, `signInShmBytes = 1 << 30`, `signInMinFreeBytes = 8 << 30`. The memory cap and the floor are guesses until S6. The rest of the host config is what Task 1's file records.

Measured correction from Task 1: Chromium declares `/config` as a volume. Stop, sweep and rollback must remove
anonymous volumes with the containers (`client.ContainerRemoveOptions{RemoveVolumes: true}`), including partial-start
failures. Task 5's `TestStopSignInLeavesNothing` must exercise the image-created volume, not just an empty mount request.

- [x] **Step 1: Write the failing daemon-free tests.**

```go
func TestSignInEnvHardensTheDesktop(t *testing.T)
// holds HARDEN_DESKTOP=true, HARDEN_OPENBOX=true, SELKIES_FILE_TRANSFERS=none, SELKIES_ENABLE_SHARING=false,
// SUBFOLDER=/sign-in/signin-abc/, CHROME_CLI=<url>, SELKIES_FRAME_ANCESTORS=<cockpit origin>,
// SELKIES_ALLOWED_ORIGINS=<desktop origin>, and no PELORUS, no PASSWORD, no CUSTOM_USER.
// Reject invalid/non-bare origins rather than passing arbitrary config syntax into the image.
func TestSignInHostConfigPublishesNothing(t *testing.T)
// no PortBindings, no Binds, no Privileged, no CapAdd, ShmSize signInShmBytes, Memory and PidsLimit set,
// NetworkMode "container:<owner>", and Init unset, because s6 must be PID 1 in this image.
func TestSignInRefusesASessionThatIsNotOurs(t *testing.T) // "signin-" + 24 hex only, as chatGPTBrowserSessionPattern.
func TestSignInRefusesToRunWithoutTheFloor(t *testing.T) // no egress image: ErrSignInNeedsTheFloor, and nothing created.
func TestFreeBytesReadsDfOutput(t *testing.T) // parses `df -B1 --output=avail /`; junk gives an error, not zero.
func TestListenerAddressComesFromTheRedirect(t *testing.T) // LoopbackCallback -> 43121 and its path; http://127.0.0.1:9100/cb -> 9100 and /cb.
```

- [x] **Step 2: Run them.** Expected: FAIL, undefined.
- [x] **Step 3: Implement.** Extract `startEgress` so `launchEgress` calls it, with `waitEgressFloor` inside. `StartSignIn` refuses without `b.egressImage`, then creates the sidecar with the label and `sleep` on the default network, as a box, starts it, runs `startEgress` on it, calls the preparation hook to install and await the callback listener, then creates the desktop on `container:<sidecar>` and waits for :3000. On any failure it removes what it made. The sweep lists by `signInLabel`.
- [x] **Step 4: Write the failing Docker tests.**

```go
func TestSignInBoxStartsBehindTheFloor(t *testing.T) // from the sidecar, a TCP dial to the shared bridge fails and :3000 answers.
func TestStopSignInLeavesNothing(t *testing.T) // no container with the label remains, and no anonymous volume.
func TestSweepRemovesEverySignInContainerLeftBehind(t *testing.T) // a box whose Aura died is gone after SweepSignIns; shell and browser boxes are untouched.
```

- [x] **Step 5: Implement the session in `cmd/aura/serve_signin_browser.go`.** `Open` checks the pattern, `AdmitBrowser`, the image, then the disk when the image is absent, and takes the identity from the context as `sandboxChatGPTBrowser.Open` does. It retains the immutable `LaunchOptions` for that session. `RedirectURI` returns the redirect it was given. `Navigate` calls `StartSignIn` with both origins and a preparation hook that writes `callback-listener.mjs`, starts it on the redirect's port and path with the state from the consent URL, and awaits listener readiness before desktop creation. `Callback` reads `{type: "callback"}`, as `chatGPTBrowserSession` does. `Close` calls `StopSignIn`. `SweepSignIns` runs at boot.
- [x] **Step 6: Run the tiers.** `go test -race ./internal/sandbox/usersandbox/ ./cmd/aura/`, then `go test -tags=docker_integration -skip 'TestSoak_ConcurrentIdentities|TestEgress_FQDNAllowlist' ./internal/sandbox/usersandbox/` with Docker up and CI=1. The 32GB soak and optional OpenSandbox FQDN binary require another host/build and remain unmeasured; the executed matrix must contain no skips.
- [x] **Step 7: Commit,** then the manifest apart. `feat(sandbox): run a sign-in in a desktop container of its own`, `chore(payload): regenerate the manifest`.

### Task 6: The stream through the cockpit

**Files:**
- Create: `internal/agui/signin_proxy.go`, `internal/agui/signin_proxy_test.go`, `internal/sandbox/usersandbox/exec_conn.go`, `internal/sandbox/usersandbox/exec_conn_test.go`, `cmd/aura/signin-relay.mjs`, `web/src/routes/SignInPage.tsx`, `web/src/routes/SignInPage.test.tsx`
- Modify: `internal/agui/server.go` wiring, `cmd/aura/serve_webui.go`, `caddy/Caddyfile`, `compose.yaml`, `.env.example`, `scripts/payload_manifest.txt`, `internal/config`, `web/src/main.tsx`, `web/src/i18n/resources.governance.ts`

**Interfaces:**
- Consumes: `SignInBox` from Task 5.
- Produces:

```go
// in agui
type SignInBrowser interface {
	Owns(ctx context.Context, owner, session string) bool
	Origins(session string) (cockpit, stream string, ok bool)
	Dial(ctx context.Context, session string) (net.Conn, error)
	Cancel(ctx context.Context, owner, session string) error
}
func (s *Server) SetSignInBrowser(b SignInBrowser)
// mounted on the parent mux behind the cockpit session, every method,
// admitted only when the request matches that session's dedicated stream origin:
//   /sign-in/{session}/
// in usersandbox
func (r *SandboxRouter) DialExec(ctx context.Context, h BoxHandle, command string) (net.Conn, error)
```

- Owner-scoped `GET /api/sign-in/{session}` returns immutable origins; `DELETE` cancels the exact route. The dedicated `/.aura-ready` read answers only the cockpit Origin, after ownership, with credentialed CORS and no upstream dial. The page checks it before framing, and links back to the connector card for paste-back.

- Cockpit route `/signing-in/:session`, with an iframe on `<streamOrigin>/sign-in/<session>/` and the flow's id in `?flow=`. The immutable stream/cockpit origins are available only after ownership verification. Caddy's existing `:8444` listener forwards `/sign-in/*` while retaining its unrelated-path 404; cockpit-origin requests for the desktop get 404 before any dial. Reuse existing `AURA_MCP_SANDBOX_ORIGIN`, `AURA_MCP_SANDBOX_PORT` and `AURA_MCP_SANDBOX_BIND`; forward the existing port setting to Aura when deriving a same-host origin. Enable the authenticated dedicated port for LAN installs as part of the approved fallback, documenting the bind change. A separate-host origin or tunnel access uses paste-back.

The tests below implement the operator's dedicated-origin choice of §4.3; no opaque-origin relaxation is made on the cockpit.

- [x] **Step 1: Write the failing tests.**

```go
func TestSignInProxyRefusesBeforeItDials(t *testing.T)
// no identity, another identity, an ended session, a malformed one: 404, and the fake's Dial count stays 0.
func TestSignInProxyNeverForwardsTheCockpitCookie(t *testing.T)
// a request with Cookie and Authorization: the fake container sees neither, nor X-Forwarded-For.
func TestSignInProxyIsolatesEveryResponse(t *testing.T)
// On the dedicated origin, CSP holds "sandbox allow-scripts allow-same-origin" and the exact cockpit frame-ancestor,
// including on static HTML/scripts and errors. Upstream CSP cannot widen it.
func TestSignInProxyRefusesTheCockpitOrigin(t *testing.T) // 404 and zero dials, including assets and upgrades.
func TestTheDesktopCannotWriteToTheCockpit(t *testing.T)
// Credentialed simple POST, form submission and same-site/different-port requests: 403 before handler side effect.
// Same-origin SPA writes and legitimate non-browser callers still work; desktop is never a trusted write origin.
func TestSignInProxyKeepsThePath(t *testing.T) // SUBFOLDER expects /sign-in/<session>/x, unchanged.
func TestSignInProxyCarriesAWebsocket(t *testing.T) // an upgrade round-trips frames both ways through the fake dialer.
func TestSignInProxyRefusesAnUpgradeFromAnotherOrigin(t *testing.T) // Origin https://evil.example: 403, Dial count 0.
func TestExecConnCarriesBytesBothWaysAndEndsWithTheProcess(t *testing.T) // Read gets EOF when the exec ends; Close kills it.
```

- [x] **Step 2: Run them.** Expected: FAIL, undefined.
- [x] **Step 3: Implement.** `httputil.ReverseProxy` with `Rewrite`, `FlushInterval: -1`, `Transport{DialContext: dial, DisableKeepAlives: true}`, and `ModifyResponse` enforcing the dedicated-origin CSP. Check ownership, exact stream request origin and WebSocket Origin before any dial. Wrap cockpit browser writes with `http.NewCrossOriginProtection().Handler`, preserving provider callbacks and non-browser callers; do not trust the desktop origin. `DialExec` is `ExecStream` over a `net.Pipe` pair (native deadlines), and `Kill` on `Close`; process exit closes the peer to yield EOF. `signin-relay.mjs` pipes stdin and stdout to `127.0.0.1:3000`. Refuse first, then dial.
- [x] **Step 4: Cockpit page.** `SignInPage` polls the flow every 2 s, closes the tab on `approved` or `failed`, cancels the flow on `pagehide`, and offers the paste-back of Task 3 from a link. Vitest: `it('closes itself when the flow ends')`, `it('cancels the flow when the tab goes')`, `it('offers the paste-back when the stream fails')`, `it('says try again when the time ran out')`.
- [ ] **Step 4a: Certificate onboarding (pending the joint Task 7 Edge session).** Add an acceptance case with ordinary browser TLS validation for both origins. An untrusted dedicated-origin certificate must offer an explicit way to open/trust that origin or use paste-back; a grey failed iframe must not be presented as a ready stream. Do not use unconditional `ignoreHTTPSErrors` as evidence for this case. Expected: trusted two-origin framing works; the untrusted case gives a usable next step.
- [x] **Step 5: Run and gate** as in Task 3.
- [x] **Step 6: Commit,** then the bundle apart. `feat(signin): stream the sign-in through the cockpit`. Commit payload edits first, regenerate the manifest in WSL and commit it separately, as in Task 5. Implementation `f89680c9f`, manifest `f8c1f2897`, bundle `3704edfb9`; Step 4a remains open.

### Task 7: Every MCP server on it

**Files:**
- Create: `web/src/governance/SignInImageGate.tsx`, `web/src/governance/__tests__/SignInImageGate.test.tsx`
- Modify: `internal/agui/mcp_oauth_api.go`, `cmd/aura/mcp_oauth_provider.go`, `cmd/aura/serve_webui.go`, `web/src/governance/McpAuthorizationPanel.tsx`, `web/src/governance/mcpAuthApi.ts`, `docs/mcp-manager.md`
- Test: `internal/agui/mcp_oauth_api_test.go`, `cmd/aura/mcp_oauth_provider_test.go`, `cmd/aura/serve_signin_browser_integration_test.go`

**Interfaces:**
- Consumes: `browserlogin.Login.Start`, `Flows.Cancel`, `OAuthSettings.SignInRedirect`.
- Produces: `mcpSignIn`, a `browserlogin.Authorizer` over `Flows`. The start route answers `{flowId, signInUrl, authorizationUrl, status}`, or `{preparing: true, reason}` when the image is absent. Routes `GET /api/governance/sign-in/image` and `POST /api/governance/sign-in/image`.

- [x] **Step 1: Write the failing tests.**

```go
func TestStartAuthorizationGoesThroughTheContainer(t *testing.T) // Flows sees LoopbackCallback; the answer holds signInUrl "/signing-in/signin-…".
func TestAPreRegisteredClientKeepsItsRedirect(t *testing.T) // MCP_OAUTH_REDIRECT_URL=http://127.0.0.1:9100/cb: the launcher is given it.
func TestAServerWithNoRegistrationReachesTheContainerThroughTheRelay(t *testing.T)
// the fallback: redirect_uri is auraOAuthRelayURL, and the state's suffix decodes to LoopbackCallback, a path the deployed relay accepts.
func TestAnHTTPSRedirectGoesToThePasteBack(t *testing.T) // a non-loopback registered redirect: via paste, no container.
func TestANonSecureCockpitGoesToThePasteBack(t *testing.T) // Origin http://192.168.1.5: paste.
func TestACrossSchemeDesktopOriginGoesToThePasteBack(t *testing.T)
// Even http://127.0.0.1 is a secure context, its HTTPS desktop iframe gets no Strict cookie: paste.
// A configured HTTP-loopback desktop matching the HTTP-loopback cockpit's scheme may use the container.
func TestAShortDiskAnswersWithTheReasonAndThePasteBack(t *testing.T) // nothing created, and the answer names the size needed.
func TestTheCockpitCallbackIsGone(t *testing.T) // GET /api/governance/mcp/authorization/callback answers 404.
func TestClosingTheSignInPageEndsTheFlowAndItsContainers(t *testing.T) // docker_integration: Cancel: flow failed, no labelled container.
func TestAFailedReauthorizationKeepsTheStoredGrant(t *testing.T) // a cancelled flow leaves Load returning the old grant.
```

- [x] **Step 2: Run them.** Expected: FAIL.
- [x] **Step 3: Implement,** then remove the old callback-based `StartAuthorization`, `callbackFor`, `configuredCallbackURL`, `handleMCPAuthorizationCallback` and the route, and shrink `isPublicOAuthCallbackRoute`. Retain request-origin derivation for the dedicated stream: starting a sign-in needs the cockpit origin, separately from the OAuth redirect, and supplies both origins in `LaunchOptions`. `AURA_WEB_PUBLIC_URL` stays while `pimReturnOrigin` reads it. `relayedFetcher` stays: it is how a server with no registration reaches the listener.
- [x] **Step 4: Cockpit.** `SignInImageGate` shows the size, 1.17 GB, and the pull's progress on first use, then opens the sign-in page. Vitest: `it('asks before the first download and shows its progress')`, `it('opens the sign-in page once the image is there')`, `it('offers the paste-back when the image cannot be had')`.
- [ ] **Step 5: S7, on the VM.** With the operator: Linear and Notion through the container, and a pre-registered GitHub client on the loopback redirect. Read Slack's rules on redirect URIs and write them down. Append to the verification file. Linear and Notion accepted on 2026-11-03 (38/72 tools, saved grants, temporary containers removed); Slack rules recorded. GitHub remains pending.
- [ ] **Step 6: Run all tiers,** update `docs/mcp-manager.md`, commit, then the bundle apart. `feat(mcp): sign every OAuth server in through the container`.

### Task 8: S4, and the fork takes a loopback redirect

The relay stays here. Everything is additive, so an older Aura keeps working beside the new image.

**Files (fork, `D:\tmp\pim-plan\fork`):**
- Modify: `src/CalendarMcp.HttpServer/Admin/AdminEndpoints.cs`, `src/CalendarMcp.HttpServer/Admin/GoogleOAuthManager.cs`
- Test: `src/CalendarMcp.Tests/Admin/` beside the existing admin tests

**Interfaces:**
- Produces, beside the existing `GET /admin/auth/{accountId}/google/start`:
  - `POST /admin/auth/{accountId}/google/start` with `{"redirectUri": "...", "clientId": "...", "clientSecret": "..."}`, answering `{authUrl, redirectUri}`. The client is kept with the pending state. The account's stored client is not touched.
  - `POST /admin/auth/google/exchange` with `{state, code}`, answering `{accountId}`, or 400 with `{error}`.

- [ ] **Step 1: S4 by hand.** The operator creates a Desktop client. With a throwaway container: consent, loopback redirect, exchange, and a refresh the next day. Write the result in the verification file. A failure comes back to the spec.
- [ ] **Step 2: Write the failing tests.**

```csharp
[TestMethod] public async Task GoogleStart_TakesALoopbackRedirect()      // authUrl holds the redirect_uri given, and a state with no relay suffix
[TestMethod] public async Task GoogleStart_RefusesAnyOtherRedirect()     // https://evil.example/cb and http://10.0.0.1/cb: 400
[TestMethod] public async Task GoogleStart_UsesTheClientItIsGiven()      // authUrl holds that client_id; the account's ProviderConfig is unchanged
[TestMethod] public async Task GoogleExchange_UsesTheRedirectAndClientOfItsState() // the token request carries both
[TestMethod] public async Task GoogleExchange_RefusesAStateItDidNotIssue() // 400, nothing stored
```

- [ ] **Step 3: Implement, and run the suite beside GreenMail and Dovecot.** Expected: 0 failed.
- [ ] **Step 4: Commit on `aura-watch`, then ask the operator to write «pubblica».** Wait for `CI Build` and `aura-publish-image` green on that SHA.

### Task 9: Google on it

**Files:**
- Modify: `internal/agui/connect_pim_api.go`, `internal/agui/connect_pim_inject.go`, `web/src/governance/PimProviderAppsPanel.tsx`, `web/src/governance/PimConnectPanel.tsx`, `web/src/governance/pimConnect.ts`, `web/src/i18n/resources.governance.ts`, `docs/mcp-manager.md`, `cmd/aura/serve_signin_browser.go`
- Test: `internal/agui/connect_pim_api_test.go`, `internal/agui/connect_pim_inject_test.go`, `web/src/governance/__tests__/CalendarConnect.test.tsx`

**Interfaces:**
- Consumes: `browserlogin.Login.Start`, Task 3's paste shape, Task 7's `SignInImageGate`, Task 8's routes.
- Produces: `pimGoogleSignIn`, a `browserlogin.Authorizer` over the PIM; `POST /api/connect/pim/accounts/{id}/auth/google/paste`.

- [ ] **Step 1: Write the failing tests.**

```go
func TestGoogleSignInGoesThroughTheContainer(t *testing.T) // start posts LoopbackCallback and the admin's app; the query reaches /admin/auth/google/exchange.
func TestSigningInAgainRewritesTheAccountsClient(t *testing.T) // after the exchange, clientId and clientSecret are the admin's current app; the id stays.
func TestACancelledSignInLeavesTheAccountsClient(t *testing.T) // no rewrite, and an account still refreshes.
func TestAWatchSurvivesSigningInAgain(t *testing.T) // db_integration: the pim_watch task is still there.
func TestGooglePasteRefusesAnotherFlowsState(t *testing.T)
```

- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.** The panel asks for a client of type Desktop app. A Web client left in place fails at Google with `redirect_uri_mismatch`, and the panel's text says which type to create.
- [ ] **Step 4: Run all tiers, commit, then the bundle apart.** `feat(pim): sign Google in through the container`.
- [ ] **Step 5: Live on the VM, through the updater.** Push per the rule, every workflow green on that SHA. The operator updates Aura and the PIM from the cockpit and connects Google with a Desktop client. Read back the account's state and a calendar read. Append to the verification file.

### Task 10: The relay goes

Only after Task 9's full sign-in on the VM (spec §7).

**Files:**
- Fork: remove `src/CalendarMcp.HttpServer/Admin/GoogleOAuthRelay.cs` and `src/CalendarMcp.Tests/Admin/GoogleOAuthRelayTests.cs`; modify `Admin/AdminEndpoints.cs`, `Admin/GoogleOAuthManager.cs`, `src/CalendarMcp.Core/Configuration/CalendarMcpConfiguration.cs`
- Aura: remove `cmd/aura/pim_relay_artifacts_test.go`; modify `internal/agui/connect_pim_api.go`, `internal/agui/connect_pim_providers_api.go`, `cmd/aura/serve_webui.go`, `compose.yaml`, `scripts/payload_manifest.txt`, `.env.example`

- [ ] **Step 1: Aura first.** Remove `PIMGoogleRelayRedirectURI`, `redirectUri` from the admin view, `returnBase`, `pimReturnOrigin`, `connectPIMGoogleCallbackRoute`, `CALENDAR_MCP_CalendarMcp__GoogleOAuthRelayUrl`, and `AURA_WEB_PUBLIC_URL` if `git grep` shows no reader. `TestPIMSidecarIsToldTheGoogleRelay` goes with what it pinned, and the commit body says so. Add `TestTheGoogleCallbackRouteIsGone`: the route answers 404.
- [ ] **Step 2: Run all tiers, commit, then the manifest apart.** `refactor(pim): drop the Google relay`.
- [ ] **Step 3: The fork.** Remove the `GET` start, `GoogleOAuthCallback`, `OAuthResultPage`, `GoogleOAuthRelay`, `GoogleOAuthRelayUrl` and `installCallbackUrl`. Run the suite. Expected: 0 failed.
- [ ] **Step 4: Commit on `aura-watch`.** Do not publish until the release carrying Step 1 has reached every known install, since an older Aura still calls the `GET` start. Say so to the operator, who writes «pubblica» then.

### Task 11: Close

**Files:**
- Modify: `prd.md`, `docs/mcp-manager.md`, `.env.example`, `docs/superpowers/plans/2026-11-02-sign-in-browser.md`
- Create: `docs/superpowers/verification/<date>-sign-in-browser-live.md`
- Modify, with the operator's yes, in `gitlab.com/Chetto1983/aura-connect`: `mcp/client-metadata.json`

- [ ] **Step 1: S5.** Nothing above depends on it, since ElevenLabs goes through the relay into the container. It decides how much of the site a later job can remove. If an ElevenLabs account is available, name `mcp.LoopbackCallback` in the document beside the relay and sign in with it. If not, the relay stays and the file says why.
- [ ] **Step 2: S6.** On the smallest install available: pull time, peak memory, free disk after. Fix `signInMemoryBytes` and `signInMinFreeBytes` from the measure, in their own commit.
- [ ] **Step 3: Live on the VM, through the updater.** The operator connects Canva, Linear and Google from `https://192.168.101.158`. Read back: grants saved, tools mounted, no labelled container left, nothing in the logs but ids. Close the tab mid-flow once. Kill the process mid-flow once, and see the sweep. Then the same with the cockpit on plain `http://`, where the paste-back must appear.
- [ ] **Step 4: Amend the PRD** after these measures, with date, evidence and what they do not prove.
- [ ] **Step 5: Gates.** `make quality-full` in WSL, `bash scripts/coverage_docker.sh`, mutation in CI on `internal/browserlogin` and `signin_proxy.go`, ≥70% killed. Push, and every workflow green on that SHA.
- [ ] **Step 6: The Cloudflare pages are not this plan.** They go in a job of their own once every known install runs this release (spec §4.6). Record that as an open item in the PRD amendment.
