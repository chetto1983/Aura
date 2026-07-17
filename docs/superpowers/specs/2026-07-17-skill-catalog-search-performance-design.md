# Skill catalog search performance design

**Status:** approved in conversation on 2026-07-17

**Scope:** Governance → Skills → Install skill catalog search

**PRD amendment:** `prd.md` Amendment #85

## Problem and measured baseline

The install panel currently keys a TanStack query directly on every input value. Each
non-empty value reaches `Installer.Search`, which starts a new
`npx skills find <query>` process. Typing `docx` with a 50 ms key delay therefore
starts four subprocesses (`d`, `do`, `doc`, `docx`) before any can finish.

Live measurements against `http://localhost:9080/` established the bottleneck:

| Path | Observed result |
|---|---:|
| Browser typing `docx` | 4 requests in 243 ms; final result after about 7.3 s |
| One Aura request using `npx skills find` | about 2.5 s |
| Cached CLI binary, telemetry disabled | about 1.0 s |
| Direct `https://skills.sh/api/search?q=docx` | median about 0.49 s |
| Mastra `skills-api` over its bundled snapshot | about 18 ms |

The Mastra mirror is not selected. Its bundled data was stale, its package was not
published, its refresh path introduces another operated service, and its audited
dependency tree contained unresolved production vulnerabilities. Aura can meet the
product latency target using the current skills.sh result source without accepting
that operational and security scope.

## Goals

1. A rapid multi-character input produces one catalog request, not one request per
   keystroke.
2. A cold live `docx` result is visible within 1.5 seconds of the final keystroke.
3. A repeated query served from Aura's cache is visible within 500 ms of the final
   keystroke.
4. Stale prefix results never replace the latest query.
5. Loading, empty, disabled, and error states are visible and accessible.
6. Catalog failure degrades through the official CLI before surfacing an error.
7. Skill installation remains on `npx skills add`; no install or validation behavior
   changes.
8. The final live Playwright audit passes all ten rubric points.

## Scope boundary

This change is a cockpit-only catalog transport improvement behind the existing
privileged endpoint:

`GET /api/governance/skills/catalog?q=<query>`

It does **not** restore the deleted model-facing `skill action=catalog`, add a new
agent tool, or change the builtin `find-skills-aura` instructions. Model-driven
self-extension continues to use `npx skills find` in the host terminal under
Amendments #51 and #52.

It also does not alter installation. A selected result is still converted to
`owner/repo@skill`, and the existing installer still runs:

`npx skills add <source> --copy -y`

## Architecture

### Request flow

```text
search input
  → trim + require 2 characters
  → 250 ms debounce
  → TanStack query with AbortSignal
  → existing authenticated Aura catalog endpoint
  → 60 s bounded cache / same-query singleflight
  → skills.sh JSON API
  → official CLI fallback on transport/schema/status failure
  → existing SkillsCatalogResult response
```

The browser continues to call only Aura. It never calls skills.sh directly, so
credentials, authorization, same-origin behavior, and the deployment opt-out remain
centralized.

### Frontend

`SkillInstallPanel` owns the raw input and a debounced trimmed value:

- debounce: 250 ms;
- minimum query length: two characters;
- no request for empty or one-character values;
- query key: the debounced value, never the raw keystroke value;
- query function: accepts TanStack Query's `AbortSignal` and forwards it through
  `searchSkillCatalog` to `getJSON`;
- stale protection: results are hidden while raw input and the debounced query differ;
- retries remain disabled because the backend owns the single controlled fallback.

The catalog area exposes distinct accessible states:

- `role="status"` plus the shared spinner while debouncing or fetching;
- the existing deployment-disabled note when `enabled=false`;
- a localized empty-result note after a successful eligible query;
- a localized destructive alert on search failure;
- result buttons only for the latest settled query.

Changing the catalog API helper from `searchSkillCatalog(query)` to
`searchSkillCatalog(query, signal?)` is additive for callers and uses the existing
optional signal support in `getJSON`.

### Backend

A focused `internal/skills` catalog client becomes the primary cockpit search
transport. `Installer.Search` preserves the discovery opt-out and empty-result wire
contract, then delegates to the client.

The client contract is:

- `GET https://skills.sh/api/search?q=<url-encoded query>`;
- two-second upstream timeout;
- `Accept: application/json`;
- non-2xx status is an error;
- response body capped at 1 MiB before decode;
- lax JSON decoding: consume `skills[].source`, `skills[].skillId`, and
  `skills[].installs`; ignore unknown fields;
- the top-level `skills` field must be present and be an array; an absent or malformed
  field is schema failure, while a present empty array is a valid empty result;
- preserve upstream fuzzy ranking order;
- drop incomplete rows and cap the result to the first 20 usable hits;
- format numeric installs into the compact string already exposed by Aura;
- successful results cached for 60 seconds in a process-local, 128-entry bounded
  cache keyed by normalized query;
- concurrent misses for the same normalized query coalesced with `singleflight`;
- every caller can still stop waiting when its own context is cancelled;
- a shared operation may finish within its two-second bound after one caller cancels,
  allowing another waiter or the cache to receive the result;
- only successful direct or fallback results enter the cache.

The existing `CommandRunner` becomes the fallback, not the primary search transport.
If the direct API fails because of timeout, network error, non-2xx response, oversized
body, or unusable JSON, Aura runs `npx skills find <query>` and keeps the existing
ANSI-stripping parser. The production command environment sets `DO_NOT_TRACK=1` and
`GIT_TERMINAL_PROMPT=0`. If both paths fail, the existing handler returns its
sanitized 502 and the panel renders the localized search error.

`AURA_SKILLS_EXTERNAL_DISCOVERY=false` remains authoritative and short-circuits
before cache, HTTP, or CLI work.

## Error and cancellation semantics

- Empty and one-character queries return an enabled empty result without external
  work.
- Browser cancellation is not rendered as an error and prevents stale state from
  committing.
- A cancelled caller does not corrupt or evict a successful shared fetch.
- The direct API never receives Aura credentials.
- Raw upstream errors and URLs do not cross the Aura HTTP boundary; existing
  sanitization remains authoritative.
- Cache state is memory-only and contains public catalog metadata only.

## Test-first implementation slices

Production code is written only after the corresponding test fails for the expected
reason.

1. **Catalog transport:** Go tests pin URL encoding, lax decode, rank preservation,
   result cap, install-count formatting, timeout/status/body-cap errors, cache TTL and
   bound, concurrent coalescing, caller cancellation, and successful CLI fallback.
2. **Installer integration:** Go tests pin opt-out and short-query no-I/O behavior,
   direct-primary behavior, fallback parsing, dual-failure behavior, and unchanged
   `npx skills add` arguments.
3. **API propagation:** Vitest first pins the optional `AbortSignal`.
4. **Panel behavior:** fake-timer Vitest tests pin the two-character minimum,
   250 ms debounce, one-call rapid typing, signal forwarding, stale-result hiding,
   and all visible states.
5. **Live build:** full Go and frontend verification runs before rebuilding the Aura
   container and its embedded web distribution.

## Playwright 10/10 acceptance rubric

The final audit runs against the rebuilt `http://localhost:9080/`, using the existing
operator login path and collecting console, page, request, and same-origin response
failures.

Each item is worth one point; completion requires **10/10**, not an average:

1. Open Governance → Skills → Install skill successfully.
2. Typing one character emits zero catalog requests.
3. Typing `docx` rapidly emits exactly one catalog request with `q=docx`.
4. An accessible loading status appears before the result settles.
5. The cold result appears within 1.5 seconds of the final keystroke.
6. The current top result is `anthropics/skills@docx`; its compact install count
   matches a same-run response from the official skills.sh endpoint.
7. Selecting that result fills the source field with the exact installable spec.
8. Replacing an in-flight query cannot render the abandoned query's rows.
9. A forced catalog 502 renders the localized error and a subsequent query recovers.
10. A repeated query appears within 500 ms, the Install control remains operable,
    and the run has zero unexpected console, page, or same-origin HTTP failures.

The Playwright report records request count and timings so the score is reproducible
evidence, not a visual judgment.

## Rejected alternatives

### Frontend-only debounce

This removes subprocess fan-out but leaves each search at roughly 2–2.5 seconds. It
does not meet the approved cold latency target.

### Mastra `skills-api` mirror

This provides sub-100 ms local reads but requires snapshot refresh, freshness
monitoring, dependency remediation, packaging fixes, and a new operated service. It
is disproportionate to the cockpit search requirement and was stale in the audited
state.

### Browser-to-skills.sh request

This would bypass Aura's discovery opt-out, server authorization boundary, cache,
fallback, body cap, and error sanitization. It is rejected.

## Out of scope

- changing search ranking or adding local semantic/vector search;
- installing or operating a catalog mirror;
- changing the model-facing skills discovery workflow;
- changing skill installation, activation, validation, or audit behavior;
- unrelated Governance or shell/share refactoring.
