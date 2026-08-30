# Progress

Last updated: 2026-08-30

This file is the durable handoff for the current work. Update it after each
meaningful implementation, verification, commit, or live-stack step so the work
survives context compaction.

## Objective

Finish GSD Phase 51, plan 51-08, without spending OpenRouter credits. Aura must
support both the local llama.cpp model `gemma-4-12b` and the operator's Ollama
bridge with `gemma4:31b-cloud`, expose measured context/cost provenance
consistently, and switch LLM endpoints/models at runtime without restarting the
Aura container.

A separate follow-on GSD phase will add provider-native OpenAI and Anthropic
routes, including subscription authentication. That work is deliberately not
being folded into Phase 51.

## Non-negotiable constraints

- Use GSD artifacts and phase workflow for the work.
- Do not run inference through OpenRouter. Use local `gemma-4-12b` or the
  operator-authorized Ollama subscription model `gemma4:31b-cloud`.
- Endpoint/model/profile changes must not restart or recreate Aura.
- Never inspect, retain, write, or commit Telegram chat content, identifiers,
  credentials, authorization data, or session material.
- Never write raw provider credentials or credential-bearing URLs to logs,
  settings, git, planning artifacts, or this file.
- Never run the retired `scripts/quality_snapshot_gate.sh` (PRD amendment #177).
- Never stage `.bg-shell/`, `.mcp.json`, or `.planning/milestone.lock`.
- The repositories under `D:/tmp` are read-only implementation references.

## GSD state

- Active command: `gsd-execute-phase 51`.
- Phase 51 has 14 of 14 plans executed, but remains `verifying`: the standard
  code review found recovery, trust-boundary, snapshot, resume-fairness, and UI
  isolation gaps that must be fixed before verifier handoff.
- Plan `.planning/phases/51-durable-delegation/51-08-PLAN.md` is complete.
- The hot-route defect is fixed and verified through the authenticated Cockpit.
- The official GSD executor reconciled plan 51-08 with Amendment #183's
  already-recorded 9.9/10 acceptance and Amendment #177's retired ledger gate.
- After Phase 51 is complete, insert and plan a provider-native phase through
  GSD. It should not move the current phase pointer while 51-08 is unfinished.

Current execution checklist:

1. PRD hot-route contract: complete.
2. RED runtime tests: complete.
3. Prepare and publish a complete model profile: implemented, verification in
   progress; committed.
4. Add the measured Ollama provider/profile and verify it live through Aura's
   existing client: implemented; focused and live tests green.
5. Align Settings batch update, cockpit, AG-UI, and Telegram runtime consumers:
   implemented; committed.
6. Race/build/frontend/live no-restart verification: complete.
7. Resume and complete 51-08: execution complete; review remediation active.
8. Fix and re-review Amendment #190 findings: in progress.
9. Run the final full matrix and repeated live E2E on the final image: pending.
10. Insert and plan provider-native subscription phase: pending.

## Committed decisions

- `ab2c576c5 docs(settings): define hot primary LLM route`
  - PRD amendment #184 defines runtime endpoint/model switching.
- `b11fa906b test(settings): expose missing hot LLM route`
  - Adds the RED runtime behavior tests.
- `7c8ce61ab docs(settings): define measured model profile`
  - PRD amendment #185 records the measured local context and provider/profile
    design contract.
- `ea5dcfbef docs(settings): define measured Ollama cloud route`
  - PRD amendment #186 records Ollama 0.33.2, the keyless OpenAI-compatible
    bridge, `/api/show` discovery, subscription cost semantics, and live-test
    boundary.
- `f4cd74342 feat(llm): publish provider model profiles atomically`
  - Adds immutable operation snapshots, provider metadata resolution, Ollama
    pricing provenance, and runner/cron/swarm integration.
- `7ab98b356 feat(settings): hot-reload complete LLM profiles`
  - Adds prepare-before-persist batch Settings updates and hot AG-UI publication.
- `e74da321d feat(telegram): follow the hot LLM cost profile`
  - Makes `/cost` follow the active snapshot without retaining Telegram data.
- `213e617f5 feat(web): switch primary LLM routes atomically`
  - Adds the three provider presets, batch save, tests, and rebuilt embedded UI.
- `37a344e9f test(llm): drain large SSE fixture requests`
  - Stabilizes large loopback SSE fixtures on Windows.
- `d6c60b408 test(settings): prove live primary route hot reload`
  - Adds the opt-in authenticated Playwright witness for Ollama and llama.cpp.
- `ce2935734 docs(llm): record measured Ollama reasoning profile`
  - Records the direct reasoning and context contradiction before implementation.
- `1f01dcdfd fix(llm): project Ollama reasoning effort`
  - Maps Aura effort to Ollama's standard `reasoning_effort` field and exposes
    the measured capability levels.
- `ae9c59fe1 fix(settings): follow active model limits`
  - Clears persisted model-limit overrides when the selected route changes.
- `23356e179 fix(web): refresh hot model metadata`
  - Invalidates active profile and reasoning metadata after a Settings save.
- `3cbd008e5 test(e2e): cover Ollama reasoning and context`
  - Extends the live Playwright witness through context, effort, reasoning, and
    final-answer assertions.
- `253c4b967 docs(llm): record inherited limit precedence failure`
  - Records the first live correction after startup env values defeated DB cleanup.
- `9adaf6e35 fix(settings): reset inherited startup limits`
  - Propagates route reset intent through prepare and clears configured flags
    before provider discovery.
- `7cd32f581 docs(llm): record composer effort race`
  - Records the intermittent immediate-select/send failure.
- `0c59905d4 fix(web): submit the current reasoning effort`
  - Makes the retained assistant-ui callback read a render-synchronized effort ref.
- `7301dfead docs(agui): record terminal answer drop`
  - Records the final-only `text_response` loss found by three live runs.
- `c9e8b3664 fix(agui): stream terminal-only answers`
  - Emits one text lifecycle for terminal-only answers without duplicating normal
    streamed responses.
- `6d651fc84 build(web): align embedded assets with lockfile`
  - Regenerates the embedded distribution after `npm ci`; a second build is
    byte-stable.
- `58685403e`, `236bf4692`, `7aee86ba3` (`docs(51-08)`)
  - Reconcile validation, delimit the superseded baseline, create the final plan
    summary, and hand Phase 51 to its verifier.
- `3c60158f2 docs(51): add code review report`
  - Records the 143-file standard-depth Phase 51 review and its 11 findings.
- `6f4f9fa1e docs(51): record delegation review gaps`
  - PRD Amendment #190 classifies confirmed gaps, accepted at-least-once
    boundaries, required regression coverage, and the final E2E completion gate.

PRD amendment #185 records these decisions:

- The live llama.cpp `/v1/models` response identifies `gemma-4-12b` and reports
  `meta.n_ctx=81920`. Aura had incorrectly advertised and budgeted 1,000,000.
- Provider identity, API/protocol, authentication, and model profile are
  separate concerns.
- A hot profile includes client, provider, model, context window, maximum output,
  prices/caps, and explicit override provenance.
- Prepare the full profile before persistence, persist it as one batch, then
  publish one immutable runtime snapshot.
- Local inference has an explicit zero/included cost instead of unknown remote
  pricing.
- Explicit context/max-output settings override provider metadata.
- Future provider-native support distinguishes OpenAI API-key Responses from
  ChatGPT subscription OAuth/Codex Responses, and uses native Anthropic Messages
  for Anthropic key/OAuth routes.
- Cost status must distinguish actual, estimated, subscription-included,
  local-included, and unknown.

## Reference inventory

Inspected local sources:

- `D:/tmp/pi/packages/ai/src/providers`
- `D:/tmp/hermes-agent`
- `D:/tmp/LibreChat`
- `D:/tmp/codex`

Relevant findings:

- pi keys models by `(provider, id)` and keeps API type, auth, context, max
  tokens, cost, caching tiers, and capabilities in the provider/model profile.
- pi's custom Ollama pattern reuses `openai-completions` at an `/v1` base URL,
  resolves keyless auth, and suppresses unsupported developer-role/reasoning
  assumptions instead of creating a separate client.
- pi distinguishes `openai` (`openai-responses`, API key) from
  `openai-codex` (`openai-codex-responses`, ChatGPT Plus/Pro OAuth).
- pi uses native `anthropic-messages` with API key or Claude Pro/Max OAuth.
- Hermes uses typed provider profiles, route-keyed metadata/context caches, and
  explicit price provenance.
- LibreChat uses descriptor-driven custom endpoints, provider-specific reasoning
  mappings, separate metadata/tokenomics, and identity-aware invalidation.
- Aura's current Slice 13 contract is an OpenAI-compatible remote/local router;
  provider-native subscriptions therefore need their own phase.

## Live measurements

Before this fix, the database route was:

- provider: `llamacpp`
- base URL: `http://aura-llm:8084/v1`
- model: `gemma-4-12b`

The sidecar metadata endpoint reports context window 81920. No OpenRouter
inference request or charge was incurred.

Ollama 0.33.2 live measurement:

- Aura's container reaches `http://host.docker.internal:11434` without restart.
- `gemma4:31b-cloud` is not listed by `/api/tags` or `/v1/models`.
- `POST /api/show` reports `gemma4.context_length=262144`, BF16, and completion,
  thinking, tools, and vision capabilities.
- Native `/api/chat` and non-streaming `/v1/chat/completions` returned their
  requested sentinels.
- Aura's `openai_compat.Client` passed real SSE completion, `/api/show` profile
  resolution, and streamed tool-call assembly against the model.
- The profile is `subscription-included` with no fabricated or inherited numeric
  token price.

Aura baseline before the software rollout:

- container PID: `75817`
- started at: `2026-08-30T01:22:21.123134167Z`
- restart count: `0`
- image: `sha256:699ce1260f16f7974f6d9885121ad8b109f22e5f867065a76d626c54f9ee95ca`

The software rollout built image
`sha256:9d6b3fef42ceecb008103e9acb1ff8852ca4889bb838a9df90f6a71aef881e93`
from the committed implementation and recreated Aura once. The post-rollout
no-restart baseline and final witness are identical:

- container PID: `46356`
- started at: `2026-08-30T09:17:19.214549326Z`
- restart count: `0`
- health: `healthy`

Authenticated Cockpit Settings then moved the runtime through Ollama and back to
llama.cpp. The Ollama witness proxy observed one keyless `/api/show` and two
`/v1/chat/completions` requests, with zero Authorization headers. The local turn
returned its requested sentinel. Playwright passed in 1.5 minutes without a
container restart. A separate Cockpit session subsequently selected the direct
Ollama route on port 11434; that operator choice was left intact.

The final software image is
`sha256:ed60b940c495248e2747b7d57adfb4d8b23c7b30dbdd1a731599dff2d92e399b`.
Its pre-drive and post-drive container witness is identical:

- container PID: `19645`
- started at: `2026-08-30T10:43:20.631079759Z`
- restart count: `0`
- health: `healthy`

One Playwright run and a separate `--repeat-each=3` run all passed. Every run
changed profiles through Cockpit, observed Ollama context `262144`, selected
`high`, asserted the actual `/agent/run` payload carried `aura.effort=high`,
received non-empty reasoning frames and the requested final sentinel, exercised
llama.cpp, and restored Ollama in `finally`. No OpenRouter request participated.

A concurrent software rollout subsequently replaced the shared container with
image `sha256:ff68727c255cb2b19d684be4bc6d7bb044459a4fa48328f2dc04bdec5bdac878`.
Two independent `--repeat-each=3` drives then passed on that image. The final
pre/post tuple is identical: PID `74261`, StartedAt
`2026-08-30T11:05:53.971427258Z`, RestartCount `0`, same image, healthy. Logs
from that baseline contain zero OpenRouter references. This supersedes the older
tuple as the current no-restart witness.

## Current implementation

The implementation below is committed in the scoped changes listed above.

### Atomic LLM runtime

- `internal/llm/runtime.go` publishes immutable `RuntimeSnapshot{Client, Config}`
  values atomically and clones mutable maps before publication.
- Runner, cron handlers, resident swarm workers, AG-UI, and Telegram snapshot the
  runtime at the start of an operation.
- In-flight operations retain their original client/profile while new operations
  observe the replacement.
- Boot captures the pre-settings fallback before applying database environment
  overlays.

### Model metadata and pricing

- `internal/llm/config.go` tracks whether context window and maximum output were
  explicitly configured.
- `internal/llm/pricing_source.go` fetches one provider model projection and can
  read OpenRouter context/max-output/pricing plus llama.cpp `meta.n_ctx`.
- `ResolveModelProfile` clones config maps, applies measured metadata only where
  no explicit override exists, assigns explicit zero local pricing, validates,
  and leaves the original config unchanged on failure.
- Provider metadata requests scope credentials by provider: an OpenRouter key is
  retained in the active configuration for a later cloud switch but is never sent
  to a local llama.cpp metadata endpoint.
- Boot profile resolution degrades to configured fallback without logging raw
  base URLs.
- Ollama model metadata uses bounded keyless `POST /api/show`; the configured
  `/v1` URL is parsed structurally to derive the native endpoint.
- Ollama local models receive explicit local-included zero cost; `*-cloud`
  models receive subscription-included provenance and no numeric price.
- Provider spend never sends a retained OpenRouter key to Ollama.
- One-shot profile, pricing, and spend clients close idle connections after use.

### Settings prepare, persist, publish

- `internal/settings/settings.go` has advisory-lock-protected `ReplaceMany` for
  atomic writes and deletes in one transaction.
- `PUT /api/settings/llm-profile` accepts only hot-profile keys.
- A reloader prepares and validates the complete client/profile before database
  mutation, persists all keys in one transaction, then executes one publication
  closure.
- Single-key PUT/DELETE uses the same prepare-before-persist behavior.
- GET after DELETE reads the active runtime value instead of a stale process
  environment overlay.
- URL validation rejects userinfo, query, and fragment data and returns generic
  errors.
- Active non-profile configuration, including a database-overlaid API key, is
  retained while a profile changes.
- Route changes reset model limits even when the stale configured value came
  from startup environment rather than a Settings row. An explicit limit in the
  same mutation remains pinned.

### Runtime consumers

- AG-UI reads model context directly from the same atomic runtime snapshot used
  for the client, preventing model/context mismatch.
- Reasoning capabilities refresh with the published profile.
- Ollama effort uses the same OpenAI-compatible endpoint: none/low/medium/high
  map to `reasoning_effort`, while xhigh/max clamp to high. No OpenRouter or
  llama.cpp-only extension is projected.
- AG-UI emits terminal-only final content before StateDelta when no text chunks
  preceded it; normal streamed answers remain non-duplicated.
- Telegram `/cost` snapshots the current runtime for model, prices, and spend
  backend. Tests use synthetic data only; no Telegram identifiers are retained.

### Frontend

- The Settings API exposes `putLLMProfile`.
- Model Settings saves dirty hot-profile fields in one batch request and saves
  unrelated settings individually.
- The default local URL is `http://aura-llm:8084/v1`.
- The routing control now has atomic OpenRouter, llama.cpp, and Ollama presets;
  Ollama selects `http://host.docker.internal:11434/v1` and
  `gemma4:31b-cloud` in the same batch.
- The embedded Web UI distribution was rebuilt after the source changes.
- Capability queries and `/api/me` are invalidated after a hot profile save.
- The external runtime's retained submit callback reads current effort from a
  layout-synchronized ref, closing the select-high/send race.

## Tests added or updated

- Runtime map cloning and replacement behavior.
- Runner, cron, and swarm operation-level snapshot behavior.
- Local profile context 81920 and explicit zero price.
- OpenRouter context/max-output/pricing projection and explicit override
  precedence.
- Failed profile resolution does not mutate source configuration.
- Local profile metadata requests never receive a retained cloud bearer token.
- Active database-overlaid API key survives a hot profile change.
- Settings batch prepares, persists, and applies exactly once.
- Settings GET after DELETE returns active fallback instead of stale env data.
- AG-UI context follows runtime replacement.
- Telegram cost output follows the current synthetic runtime profile.
- Frontend batch endpoint and one-request Model Settings behavior.
- Ollama `/api/show` projection, malformed/ambiguous context rejection, local vs
  cloud cost provenance, credential isolation, Settings publication, and wire
  compatibility.
- Opt-in live Ollama tests for profile discovery, SSE completion, and streamed
  tool calls through Aura's production client.
- Opt-in authenticated Playwright route witness that publishes Ollama and
  llama.cpp profiles, drives a real turn on each, asserts the request effort,
  reasoning frames, active context and final answer, and restores Ollama in
  `finally`.
- The large SSE fixture drains the POST body before returning its 70 KB response,
  preventing Windows from resetting and truncating the loopback connection.
- AG-UI translator coverage for terminal-only content followed by StateDelta,
  alongside the existing no-double-stream regression.

## Verification status

Green:

- Focused Go tests for `internal/llm`, `internal/agui`,
  `internal/channels/telegram`, `internal/settings`, `cmd/aura`, runner, cron, and
  swarm.
- `go vet ./...`
- `go build ./cmd/aura`
- `npm run typecheck`
- `npm run build`
- `npm run lint`
- Full frontend test run after clean `npm ci`: 226 files, 1,905 tests passed.
- Frontend coverage on final HEAD: 91.12% statements, 85.14% branches, 90.30%
  functions, 93.10% lines.
- Split Model Settings routing tests: 2 files, 25 tests passed.
- The previously failing 70 KB SDK stream test passed 20 consecutive runs after
  the fixture correction, and `internal/llm/openai_compat` passes in the full run.
- WSL race matrix passed for `internal/llm/...`, `internal/agent/prompt`,
  `internal/agui`, `internal/settings`, and `cmd/aura`; the broader earlier
  runtime/cron/swarm/channel matrix also remains green.
- Full Linux/WSL `go test -count=1 ./...` passed after all fixes.
- `git diff --check`
- Live `gemma4:31b-cloud` gate: profile discovery, streaming completion, and
  streaming tool call all passed; goleak passed after closing idle metadata
  connections.
- Rebuilt `aura:local` from the committed tree and reached healthy on the new
  image.
- Playwright live hot-route gate passed four consecutive times; PID, start time,
  restart count, image, and health remained unchanged.
- `npm run build` passed twice after `npm ci` with 4,331 modules and zero
  regenerated diff.
- `TestProductionContainerArtifactsMatchFatImageContract` passed.

Needs completion:

- Remediate the confirmed Amendment #190 findings with focused fault-injection
  and adversarial tests, keeping the accepted exactly-once residual unchanged.
- Re-run GSD code review after remediation and update `51-REVIEW.md` from the
  measured result.
- Re-run the complete WSL Go/vet/build/race and web lint/typecheck/test/build
  matrix, then rebuild/deploy the final image.
- Run the repository Playwright E2E repeatedly against that final image and
  verify the container PID/start/image/restart tuple remains unchanged during
  endpoint switches, with zero OpenRouter traffic.
- Native Windows `go test ./...` passes every changed package but still exits
  non-zero on three unrelated portability assertions: two `internal/agent` tests
  compare `%TEMP%` paths using POSIX separator assumptions, and one
  `internal/agent/tools` test expects POSIX mode `0600` from Windows. These files
  are outside the implementation diff. The authoritative Linux/WSL full suite is
  green; do not widen this phase into unrelated Windows-only test maintenance.
- Run the GSD phase review/verifier and complete the phase only on a passing
  verdict.

## Files and staging

The implementation and rebuilt Web UI are committed. The remaining untracked
local files are `.bg-shell/`, `.mcp.json`, and `.planning/milestone.lock`; they
must remain outside git.

For subsequent commits:

- Keep the implementation and tests in scoped atomic commits.
- Update the Phase 51 plan/summary/state only from measured verification.
- Do not stage `.bg-shell/`, `.mcp.json`, or `.planning/milestone.lock`.
- Do not stage or commit any messaging session or account material.

## Next actions

1. Implement the Amendment #190 fixes in atomic commits and add focused
   regression tests for every confirmed failure window.
2. Re-run GSD code review, the full verification matrix, and the repeated live
   Playwright E2E on the final deployed image.
3. Run the GSD verifier and mark Phase 51 complete only after every gate passes.
4. Insert, discuss, specify, and plan the provider-native subscription phase via
   GSD after Phase 51 is complete.

## Resume commands

Run from `D:/Repo/Aura`:

```powershell
go test ./internal/llm/openai_compat -run TestSDKStreamAccumulatesSplitToolCallAndUsage -count=1
go test ./...
npm run lint
npm run typecheck
npm test
npm run build
git diff --check
git status --short
```

Race verification:

```powershell
wsl bash -lc 'cd /mnt/d/Repo/Aura && go test -race ./internal/llm ./internal/agui ./internal/runner ./internal/cron/handlers ./internal/swarm ./internal/channels/telegram ./cmd/aura'
```
