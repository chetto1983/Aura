---
phase: quick-chatgpt-plan
reviewed: 2026-10-02T18:31:54Z
depth: deep
files_reviewed: 61
files_reviewed_list:
  - internal/chatgptplan/service.go
  - internal/chatgptplan/oauth.go
  - internal/chatgptplan/store.go
  - internal/chatgptplan/files_unix.go
  - internal/chatgptplan/files_windows.go
  - internal/llm/chatgpt/client.go
  - internal/llm/chatgpt/request.go
  - internal/llm/chatgpt/stream.go
  - internal/llm/chatgpt/models.go
  - internal/llm/chatgpt/models_bounds.go
  - internal/llm/chatgpt/media.go
  - internal/llm/chatgpt_profile.go
  - internal/llm/model_catalog.go
  - internal/llm/pricing_source.go
  - internal/llm/openai_compat/stream_idle_shared.go
  - internal/llm/openai_compat/httperror.go
  - internal/llm/spend.go
  - internal/runner/runner_identity_chatgpt.go
  - internal/runner/runner_identity_llm.go
  - internal/agui/settings_chatgpt.go
  - internal/agui/settings_api.go
  - internal/agui/settings_llm_models.go
  - internal/agui/server.go
  - internal/agui/idempotency_http.go
  - cmd/aura/serve_chatgpt.go
  - cmd/aura/serve_settings.go
  - cmd/aura/serve_webui.go
  - cmd/aura/chat_boot.go
  - cmd/aura/llm_client.go
  - cmd/aura/serve_provisioning_openrouter.go
  - web/src/settings/ChatGPTPlanConnection.tsx
  - web/src/settings/chatgptPlanApi.ts
  - web/src/settings/ModelSettingsPanel.tsx
  - web/src/settings/ModelPicker.tsx
  - web/src/settings/SettingField.tsx
  - web/src/settings/modelSettingsDefs.ts
  - web/src/settings/settingsApi.ts
  - web/src/settings/useModelCatalog.ts
  - web/src/i18n/resources.settings.ts
  - web/src/settings/SettingsWorkspace.tsx
  - web/src/settings/settingsSections.ts
  - cmd/aura/chatgpt-browser.mjs
  - cmd/aura/serve_chatgpt_browser.go
  - cmd/aura/serve_browser_live.go
  - cmd/aura/serve_agui.go
  - internal/chatgptplan/browser.go
  - internal/chatgptplan/browser_service.go
  - internal/agui/browser_live.go
  - internal/sandbox/usersandbox/docker_backend_exec.go
  - web/src/settings/useChatGPTPlanConnection.ts
  - internal/sandbox/usersandbox/router_tools.go
  - internal/sandbox/usersandbox/router.go
  - internal/agui/auth.go
  - docker/aura-sandbox/browser-relay.mjs
  - docker/aura-sandbox/agent-browser.sh
  - web/src/routes/BrowserLivePage.tsx
  - web/src/browserLive/useBrowserLive.ts
  - web/src/browserLive/BrowserLiveView.tsx
  - web/src/browserLive/liveInput.ts
  - web/src/components/computer-use.tsx
  - web/src/main.tsx
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# ChatGPT plan integration: adversarial code review

**Depth:** deep
**Files reviewed:** 61, including the VM browser delta and its production call chains
**Status:** clean after correction recheck

## Narrative Findings (AI reviewer)

The review traced OAuth registration, signed identity verification, refresh and revocation, protected storage, Responses input and terminal stream handling, runtime publication, principal ownership, HTTP mutation ingress, and frontend connection/catalog state. The follow-up also traced the VM-local Chromium callback, process cleanup, cancellation and startup publication, private live-view metadata, and the member authentication boundary. Source review was read-only; executable broad checks belong to the implementation orchestrator. Passing compilation or unit tests was not used as correctness evidence. Findings fixed during the review are retained separately below.

### CR-01: Clicking the active ChatGPT provider disables the account route

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**File:** `D:/Aura/web/src/settings/ModelSettingsPanel.tsx:266`
**Related:** `web/src/settings/RouteToggle.tsx:38`, `web/src/settings/ChatGPTPlanConnection.tsx:86`
**Issue:** The provider callback always clears `chatGPTReady`. The existing toggle deliberately invokes the callback for the current provider when its active button is clicked. The connection component stays mounted with `ready=true` and a stable callback, so its connection effect does not report readiness again. The catalog, model selection and Save are then disabled despite valid credentials.
**Fix verified:** Readiness is now cleared only when changing providers. The existing current-provider route reapplication remains intact, and the added regression covers clicking already-selected ChatGPT.

### CR-02: Ordinary members have no usable route to their own required OAuth connection

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/cmd/aura/serve_webui.go:215`, `D:/Aura/web/src/settings/SettingsWorkspace.tsx:31`, `D:/Aura/web/src/settings/settingsSections.ts:43`
**Issue:** The primary route is deployment-wide but its OAuth credentials are per Aura identity. A regular member needs their own ChatGPT grant after an admin selects this provider. The initial implementation rendered its only connection UI in the model pane hidden from members by the `identity.create` admin check. Default members receive `governance.write`, so the original endpoint gate did not deny all members; authenticated owners without that capability also lacked API access. The absent member entry point made the required owner credential impossible to obtain through the app.
**Fix verified:** The parent mount now allows authenticated owner connection/status/disconnection. The non-admin personal settings view renders `ChatGPTPlanConnection`; it exposes neither model nor provider administration. Backend regression verifies owner access and refusal of global profile writes.

### WR-01: Catalog SDK error adaptation still drops the provider request ID

**Classification:** WARNING
**Disposition:** corrected; no longer open.
**File:** `D:/Aura/internal/llm/chatgpt/stream.go:146`
**Issue:** The `*openai.Error` branch constructs `HTTPError` using only status and body. Catalog requests use their own size-bound middleware, so their admission errors reach this branch and lose the response request ID even after the shared HTTP-error type gained request-ID support. That ID is needed to diagnose direct-route permission/routing failures.
**Fix verified:** The SDK branch retains a bounded, sanitized request ID, bounded body and retry-after value. Added tests assert metadata preservation and token redaction.

## Findings corrected during this review

The three findings above and the corrections below were inspected in the final source. No unresolved findings remain in the reviewed scope. Live OAuth/inference and broad executable release verification remain the orchestrator's separate responsibility.

- **BLOCKER — Mixed runtime snapshots could borrow the deployment OpenRouter client.** Original `internal/runner/runner_identity_llm.go:126` classified ChatGPT using one runtime read and obtained its client using another. A concurrent provider switch could bypass owner-key and credit resolution. The resolver now classifies and uses one immutable client/config publication.
- **BLOCKER — Pending reauthorization closed the OAuth popup prematurely.** Original `web/src/settings/ChatGPTPlanConnection.tsx:50` closed the popup for retained connected credentials even when the new flow was still pending. It now requires `status === 'approved'`.
- **BLOCKER — Enabling an earlier-declined plan grant did not explicitly request consent.** Original `internal/chatgptplan/service.go:129` reused the client but omitted consent parameters. It now adds `prompt=consent` only when the retained registration lacks plan-use scope and rechecks the grant.
- **BLOCKER — A failed remote revocation left the UI claiming a live connection after local credentials were cleared.** Original `web/src/settings/ChatGPTPlanConnection.tsx:166` retained its connection state on error. It now refreshes status and keeps the revocation warning visible.
- **WARNING — Connection responses were cacheable.** `internal/agui/settings_chatgpt.go:42` now applies `Cache-Control: no-store` and no-referrer to connection responses, including login URLs containing an ID-token hint.
- **WARNING — Direct admission diagnostic text and request IDs were discarded.** `internal/llm/openai_compat/httperror.go` now handles string `detail` errors and preserves a bounded request ID. The catalog SDK branch in WR-01 was also corrected.
- **WARNING — Revoked stored sessions hid the sign-in recovery action.** Catalog refresh can clear invalid-grant credentials after initial status was loaded. The connection UI now offers an explicit reconnect action even while the cached status still says approved.

## VM browser follow-up: corrected findings

The frozen follow-up was reviewed through its process, service, API and UI boundaries. All findings below are corrected and excluded from the open-finding counts. No source file was edited by this reviewer.

### CR-03: Cancelling startup could cancel the next login too

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/web/src/settings/useChatGPTPlanConnection.ts:113`, `D:/Aura/internal/chatgptplan/browser.go:81`
**Issue:** Cancelling before the first POST returned released UI readiness without a server selector. A second POST could reuse the same backend flow; the first response's delayed DELETE then killed the second login. Separate tabs also shared this flow.
**Fix verified:** The frontend retains startup and cleanup barriers, including remounts. The server associates the flow with its idempotency attempt and joins the old flow before replacing a different attempt. Cancellation compares the exact browser route, preventing a late DELETE from cancelling a replacement. Known-selector popup navigation failures also request cleanup.

### CR-04: An immediate returning callback deadlocked navigation

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**File:** `D:/Aura/cmd/aura/chatgpt-browser.mjs:45`
**Related:** `cmd/aura/serve_chatgpt_browser.go`, `internal/chatgptplan/browser.go`
**Issue:** The callback HTTP response remained open until browser cleanup. The manager awaited the navigation-ready event before handling the callback, while `agent-browser open` awaited the callback page's load. Returning authorization could redirect directly to loopback, forming a circular wait.
**Fix verified:** A state-validated callback now emits its private event and immediately ends HTTP with a neutral message. The account status remains authoritative. The orchestrator measured the production helper and installed VM Chromium against a controlled HTTP 302: callback preceded navigation-ready, the process exited successfully, and the temporary profile was removed. This measures redirect handling, not OpenAI consent.

### CR-05: Cancellation after token validation could still save credentials

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/internal/chatgptplan/oauth.go:54`, `D:/Aura/internal/chatgptplan/browser.go:210`
**Issue:** The unlocked exchange refactor checked only flow identity and expiration before saving. Cancellation after the last JWKS operation could leave a valid credential result; a free store lock did not inspect the cancelled context and the new grant could be saved.
**Fix verified:** Callback checks cancellation before commit and again after acquiring the store lock. Browser cancellation invalidates the matching service flow, including an exchange in progress. The pending-pointer comparison rejects displaced flows. A realistic fixture cancels when the completed JWKS response closes; new and retained accounts remain unchanged, and removing the guards makes those regressions fail.

### CR-06: Cancellation could ignore its caller's deadline before cleanup

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/internal/chatgptplan/browser.go:159`, `D:/Aura/internal/chatgptplan/browser.go:215`
**Issue:** Synchronous invalidation preceded the cancellation handler's context select. Waiting for authorization publication or a service mutex held by another account could block beyond the caller's deadline.
**Fix verified:** Cancellation signals the flow immediately. Its invalidation worker waits for safe authorization publication without holding the global manager lock; the request may return on its deadline. Flow cleanup joins invalidation before publishing completion. Held-service-lock and delayed-helper regressions cover the two independent waits.

### CR-07: Readiness could report a cancelled route or reject a completed login

**Classification:** BLOCKER
**Disposition:** corrected; no longer open.
**File:** `D:/Aura/internal/chatgptplan/browser.go:129`
**Related:** `web/src/settings/useChatGPTPlanConnection.ts:255`
**Issue:** A ready event alone did not distinguish a replaced browser from successful authorization that completed before the POST caller resumed. An unconditional cancellation check then treated normal cleanup after approval as startup failure. Reading approval before checking context also left a race between those observations.
**Fix verified:** `startResult` captures the context result before its mutex-protected approval observation. Verified completion returns `approved` with no browser URL; a cancelled, unapproved flow returns an error. Approval is published only after `Service.Callback` succeeds. The frontend accepts the completed response, closes the popup and refreshes account status without navigating to a dead route.

### WR-02: Cleanup failures after normal completion were never observed

**Classification:** WARNING
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/internal/chatgptplan/browser.go:245`, `D:/Aura/cmd/aura/chat_boot.go:113`
**Issue:** Failed Chrome/profile cleanup was retained only in a pending entry and an aggregate error. Normal completion removed that entry, and production wiring discarded the manager without calling its error-reporting `Close`.
**Fix verified:** The manager immediately logs a sanitized cleanup warning without provider output, URLs, codes or tokens. Wiring retains the manager and explicitly joins `Close` during shutdown. A valid-callback regression observes the warning before shutdown and preserves the successfully verified account grant.

### WR-03: Forced browser cleanup could wait indefinitely on Docker

**Classification:** WARNING
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/cmd/aura/serve_chatgpt_browser.go:178`, `D:/Aura/internal/sandbox/usersandbox/docker_backend_exec.go:301`
**Issue:** After its grace period and forced termination, browser cleanup awaited a handle whose final Docker inspect used an unbounded background context. An unreachable daemon could prevent retries and shutdown from joining the flow.
**Fix verified:** The backend's final inspect now has a five-second context. The existing termination exec is also bounded; the adapter joins its protocol reader and process completion and reports unsuccessful cleanup.

### WR-04: Live-view metadata exposed OAuth hints and callback query data

**Classification:** WARNING
**Disposition:** corrected; no longer open.
**Files:** `D:/Aura/internal/agui/browser_live.go:66`, `D:/Aura/internal/agui/browser_live.go:266`
**Issue:** Returning authorization URLs contain an ID-token hint, and callback URLs contain authorization codes and state. The relay forwarded complete URL/tab metadata into the SPA even though its login response now contained only a private viewer route.
**Fix verified:** Private login streams use no-store and no-referrer. Each valid NDJSON message passes through recursive metadata sanitation before SSE emission: absolute HTTP(S) URL strings lose query, fragment and user-info, including nested tab URLs. Invalid JSON is discarded. This protects URL metadata; JPEG frames remain the actual page pixels and are not represented as redacted images.

## Final VM boundary recheck and limits

The production listener and Chromium run in the initiating identity's sandbox, with the exact HTTP `127.0.0.1:<port>/auth/callback` URI supplied to authorization and exchange. The cockpit streams that browser through Aura's authenticated, owner-checked routes. Reserved login sessions do not require agent-run capability, allowing an ordinary member to connect their own account; unrelated browser sessions retain that capability gate. No GitHub Pages callback substitution is involved.

No open BLOCKER or WARNING remains in the reviewed local/VM source scope. The callback-ready ordering, cancellation publication and context deadlines, credential commit guard, metadata sanitation, and cleanup/shutdown joins were rechecked in the final source. Implementation-agent regressions and the orchestrator's real Chromium redirect measurement support their stated boundaries; they do not establish human OpenAI authentication, consent, real account catalog access, or a completed Responses/tool inference turn. Those live acceptance criteria remain open and this code-review disposition does not close E2E-05.

## External contracts consulted

[OpenAI Responses requirements](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations), [account models and completed inference](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference), [registration ownership and renewal](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions), [consent and direct-route recovery](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery), and [token response fields](https://developers.openai.com/siwc/token-sharing-open-source/token-reference).

The VM follow-up also consulted [OpenAI's loopback registration contract](https://developers.openai.com/siwc/token-sharing-open-source/sign-in), [the documented credential-transfer procedure for self-hosted VMs](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms), and the installed [agent-browser v0.38.1 navigation implementation](https://github.com/vercel-labs/agent-browser/blob/v0.38.1/cli/src/native/browser.rs#L1114), which awaits browser lifecycle events for full navigation.

_Reviewer: Codex (gsd-code-reviewer)_
_Review artifact only; no source modifications or commits._
