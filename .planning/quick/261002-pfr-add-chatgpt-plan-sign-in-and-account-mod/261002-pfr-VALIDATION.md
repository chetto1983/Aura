# ChatGPT plan validation — 2026-10-02

Status: implementation and final local gates passed. PRD measurement commit `6cbfc24c6` exists; implementation push, CI/deployment verification and human account consent remain outstanding. `compose.yaml` has no diff.

## Executed checks

All executable Go and frontend checks below ran in WSL Ubuntu against the earlier local-callback revision. They establish the initial OAuth/Responses foundation; a fresh matrix is required for the current remote-browser source and embedded bundle.

| Check | Measured result | Scope |
| --- | --- | --- |
| Go vet and build | PASS, `./...` | Full repository compilation and vet |
| Go race | PASS, `./...` | Unit matrix; no account grant or inference |
| Go lint | 0 issues | Touched command, AGUI, runner, LLM, OAuth and settings packages |
| OAuth race/goleak/coverage | PASS; 89.2% | Fixture registration, identity validation, isolation, protected persistence, refresh/revocation |
| Responses race/goleak/coverage | PASS; 95.2% | SDK request/catalog/stream fixtures; terminal event and tool round projections |
| Mutation spot checks | OAuth 5/6 killed (83.3%); Responses 4/4 killed (100%) | Bounded targeted mutations, not an exhaustive mutation score |
| Frontend regression | 73/73 PASS | Ten focused test files |
| New frontend UI/API coverage | 95.65% statements; 86.27% branches; 100% functions/lines | Connection component and API surface |
| Frontend lint | 0 errors/warnings | Sixteen touched source/test files |
| Production UI build | PASS | TypeScript and Vite; embedded bundle frozen after the active-provider fix |
| Chromium UI fixture | 1/1 PASS, 8.6 seconds | Popup/callback, account model label and slug, save, reload/route memory, disconnect and mutation keys; session, settings and OpenAI are simulated |
| Dead-code and model/capability contracts | PASS | Existing repository commands |
| govulncheck | No vulnerabilities affecting called code | One uncalled dependency advisory reported |
| Live local boot/sign-in boundary | 1/1 PASS, browser test 2.3 seconds | Real Aura binary, real Authula, 109 migrations on a disposable Postgres, actual official authorization URL and forged callback rejection |
| Full disposable integration coverage | PASS; 47,779/53,856 statements (88.7%); package-local policy PASS | `scripts/coverage_docker.sh`, unit + `db_integration`, fresh Postgres with 109 migrations; source/bundle frozen |

The first broad coverage attempt was invalidated by edits and a frontend rebuild during compilation: generated embed files disappeared and an earlier compile saw inconsistent HTTP error definitions. It was rerun only after source and bundle were frozen. Those compile failures are not green coverage evidence.

The local browser boundary test used a temporary HTTPS terminator through the existing `web/e2e/https-proxy.mjs`, a fresh fixture operator and disposable runtime directories. It verified:

- Authula sign-in against the actual daemon, then the actual model-routing Settings screen.
- Login response HTTP 200, `Cache-Control: no-store` and an idempotency key.
- Navigation to `https://auth.openai.com` with `dynamic_agent_client`, S256 PKCE, the plan scope and exact local callback `http://127.0.0.1:19080/auth/callback`.
- An unissued state/code callback returned HTTP 400 with no-store and did not echo the forged code.
- The authenticated disconnect endpoint returned HTTP 200.

This stops before human OpenAI authentication/consent. It proves neither token issuance, account entitlement, the real model catalog nor a completed Responses inference/tool round. Shell runtime-health and completed-profile fixtures used by the standard browser helper do not count as readiness evidence. Initial attempts failed in the harness (HTTP CSRF/cookie handling and an incorrect Settings section/role selector); HTTPS and the observed `settings=model`/radio selectors corrected those issues without weakening production authentication.

## Existing MCP bridge and the requested VM

Read-only SSH inspection of the user-provided VM succeeded. Aura runs behind Caddy on HTTPS; the daemon listens at `0.0.0.0:9080` inside its container and publishes ports 9080–9081 only on host loopback. The running Aura image revision was `9f20327731dfef9d6322f040cb28d8f2807c314c`, with Compose working directory `/opt/aura`. No VM setting, image or stored credential was changed.

Read the actual [aura-connect repository](https://github.com/chetto1983/aura-connect), including README, `lib/relay.js` and MCP relay source, together with `internal/mcp/oauth_cimd.go` and `docs/mcp-manager.md`:

- MCP's fallback uses the published metadata document as its client ID and GitHub Pages as its registered callback.
- Aura packs `<state>.<base64url(target)>`, the page checks transport and exact callback paths, then forwards the original callback query to the target install.
- The page accepts HTTPS targets or loopback HTTP. Its MCP callback allowlist does not include ChatGPT's `/auth/callback`.
- This requires the authorization provider to accept that registered HTTPS redirect. It cannot independently change where OpenAI delivers its callback.

Official OpenAI sources fetched on the same date:

- [OSS registration and sign-in](https://developers.openai.com/siwc/token-sharing-open-source/sign-in): HTTP callback on `127.0.0.1` from initial registration; only the port may vary. Exact URI must match the token exchange.
- [Self-hosted VMs](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms): local OAuth with the same client/user/workspace, then protected credential transfer over a secure channel while preserving the VM host ID; later refresh belongs to the VM.
- [Website sign-in](https://developers.openai.com/siwc/website): provisioned client and exact registered callback, with a documented identity-only contract. That alone does not authorize plan inference.
- [Request a client ID](https://developers.openai.com/siwc/request-client-id): currently provisioned to selected commercial partners.

No authorized HTTPS ChatGPT plan client was supplied. The revised implementation instead reuses Aura's existing per-owner sandbox Chromium and authenticated BrowserLivePage. The cockpit opens a same-origin `/browser/chatgpt-<random>` route; Chromium in that owner's sandbox visits the official authorize page. A listener in the same sandbox receives the documented loopback redirect and the existing exec stream transports its query privately to the owner-scoped OAuth service. No Compose change, additional public port, GitHub callback substitution or manual credential transfer is needed. The public Aura callback route was removed.

A separate read-only compatibility review confirmed this conclusion against the official registration, VM, website and Codex app-server guides. App-server consumes an already obtained access token and supplies no alternate remote sign-in mechanism. No HTTPS rejection was measured; the finding is the documented protocol requirement, not a live rejection result. The interest form for remotely hosted apps does not supply a public HTTPS plan-usage contract by itself.

## Outstanding acceptance

1. Commit and push the reviewed implementation; check CI/image publication for that exact commit and the VM's automatic update.
2. Run deployed browser/login/cancellation E2E and complete human OpenAI consent for the selected account and workspace.
3. Read the actual account catalog, save an available slug, measure a completed turn and local Aura tool round, and verify provider reload.

The GSD task and PRD release contract must not be marked complete from the fixture results above.

## Remote browser measurements on the actual VM

The installed owner sandbox provides Node 24.20, native `agent-browser` 0.38.1 and Chromium. Production-helper probes exercised that actual browser:

| Measurement | Result | Boundary |
| --- | --- | --- |
| Ephemeral loopback listener | Actual `127.0.0.1` callback reached Chromium's sandbox | No public callback or SSH tunnel |
| Official authorization navigation | OpenAI Welcome back loaded with email and existing provider choices | No human authentication or consent |
| Existing browser frame transport | First frame observed after 26 ms | Actual native browser stream |
| Wrong callback state | HTTP 400 | No token request |
| Controlled valid-state callback | Exit 0; temporary Chrome profile removed | Synthetic code/client ID, no real grant |
| Cancel during startup | Exit 0 in 949 ms; temporary profile and stream removed | Actual Chromium cleanup |
| Already-redirecting browser | Events `listening,callback,navigated`; callback preceded navigation-ready; exit 0; profile removed | Local HTTP 302 fixture with real Chromium and production helper |

The last probe verifies the returning-login fix: the helper answers immediately with a neutral callback page so Chromium's load event can complete. These measurements establish browser/listener placement, transport and cleanup on the VM. They do not prove token issuance, entitlement, account models or inference.

Current code compares the exact route on cancellation, joins cleanup on replacement/shutdown, and checks cancellation before credential commit. Private browser SSE is no-store/no-referrer and strips query, fragment and user-info from URL metadata. This does not claim to redact rendered page pixels. Installed navigation behavior was checked in the [primary browser source](https://github.com/vercel-labs/agent-browser/blob/v0.38.1/cli/src/native/browser.rs).

Targeted root command/AGUI race tests and the Docker exec-inspect deadline regression passed. Browser lifecycle mutation overlays killed 9/9 targeted mutants without changing production source. Final totals will be recorded after source/bundle freeze. A broad coverage run started before the last review was deliberately terminated when production sources changed; its partial output is not passing coverage evidence.

## Final revision measurements

Source and bundle are frozen after the approved-before-startup response fix and the standard-library map-copy lint correction. Neither interrupted broad run is green evidence.

| Check | Result | Scope |
| --- | --- | --- |
| OAuth/browser lifecycle race | PASS, repeated three times; 92.4% package coverage | Fixture grant plus lifecycle; browser functions 100% before equivalent map-clone lint correction; targeted race/vet rerun after correction |
| Browser mutation | 9/9 killed (100%) | Final lifecycle guards, source-safe overlays |
| Frontend focused suite | 46/46 PASS | Connection, cancellation, API, model route and member entry point |
| Frontend coverage | 95.76% statements, 88.26% branches, 97.61% functions, 100% lines | New UI/hook/API; UI/API 100% |
| Production bundle | PASS | Final TypeScript/Vite build, unchanged after verification |
| Chromium UI fixture | 3/3 PASS, 21.2 seconds | Actual Settings/BrowserLivePage/input, same-origin login, cancellation, approved returning account; auth/settings/OpenAI remain fixtures |
| Exact frontend pre-push | PASS | Full lint/typecheck/format; ownership Prettier also passed |
| jscpd | 823 files, 0 clones | Existing zero-threshold configuration |
| Actual full-stack login boundary | 1/1 PASS, browser 5.8 seconds, total 13.6 seconds | New actual Aura binary and embedded UI, real Authula, disposable Postgres/109 migrations, actual sandbox Chromium/OpenAI authorize page/frame transport, popup-close cancellation, cancelled session HTTP 404 and disconnected status |
| VM operator authentication | 1/1 PASS, total 6.7 seconds | Actual HTTPS VM, configured existing Authula credentials and real settings API; no deployment change |
| Final Go vet/build/race | PASS, `./...` | Source and embedded bundle frozen; no account grant |
| Final touched-package lint | 0 issues | Commands, AGUI, OAuth, LLM, runner, settings and sandbox |
| Final repository contracts | PASS | Deadcode, capabilities, embedding/model contracts, vulnerability scan and 3,924 tracked source files within 600 LOC |
| Final full disposable integration coverage | 48,017/54,095 statements (88.8%); package policy PASS | Fresh disposable Postgres with 109 migrations, unit plus `db_integration`; does not replace separate Docker/ArcadeDB authorities |

The first current full-stack browser attempt used an incorrect harness assertion of HTTP 403 for a cancelled stream. The production owner check deliberately returns HTTP 404 `invalid_session`; the assertion was corrected to that inspected contract, then the test passed. Production authorization was unchanged. The standard shell helper fixtures still do not prove runtime readiness. Neither full-stack run performed human OpenAI consent or account inference.
