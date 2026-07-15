---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 12
type: execute
wave: 6
depends_on: ["37F-10"]
files_modified:
  - cmd/aura/serve_webui_share.go
  - cmd/aura/serve_webui.go
  - cmd/aura/share_public_route_test.go
  - internal/agui/share_public_route_test.go
autonomous: true
requirements: [WEBSHARE-02]

must_haves:
  truths:
    - "GET /s/{token} and its subpaths are reachable without a session; every other route stays behind RequireAuth"
    - "POST /api/shares public minting is gated by RequireCapability(share.public) at the mount"
    - "/s/{token} falls through to the SPA shell — it is NOT in fallbackExcludedPrefixes"
    - "isPublicShareRoute is fail-closed: non-GET methods and non-/s/ paths return false"
    - "cmd/aura/serve_webui.go stays under 600 LOC"
    - "The internal tier is mounted bare — RequireAuth only, no capability"
    - "GET /api/shares/{id}/data and GET /api/shares/{id}/asset/{assetID} are mounted AUTHENTICATED — they are explicit NON-members of isPublicShareRoute, so an anonymous caller gets 401/302 (SC4 row 4)"
    - "/shared/{id} — the internal-tier page — is NOT matched by the /s/ predicate, proven by test, not by eye"
  artifacts:
    - path: "cmd/aura/serve_webui_share.go"
      provides: "sharePublicCapability const + isPublicShareRoute + registerShareRoutes (parent mux)"
      min_lines: 40
    - path: "cmd/aura/serve_webui.go"
      provides: "one register call + one PublicRoute chain entry"
      contains: "isPublicShareRoute"
  key_links:
    - from: "cmd/aura/serve_webui.go"
      to: "cmd/aura/serve_webui_share.go"
      via: "the PublicRoute chain entry + the register call"
      pattern: "isPublicShareRoute|registerShareRoutes"
  prohibitions:
    - "MUST NOT add /s/ to fallbackExcludedPrefixes() — D-03 requires /s/{token} to fall through mux.Handle(\"/\", static) to the SPA shell. Adding the prefix (the reflex move) would 404 the public page. This is an explicit NON-ACTION."
    - "MUST NOT put the capability const, the predicate, or the mounts in serve_webui.go — it is at 593/600 and the precedent (serve_webui_composer.go / _musr.go / _voice.go) is explicit"
    - "MUST NOT rely on RequireCapability at the mount as the only public gate — auth.go:282 makes it vanish on loopback; the in-handler kill-switch (plan 37F-10) is the second gate"
    - "MUST NOT gate the internal tier on any capability"
    - "MUST NOT make isPublicShareRoute match a non-GET method or any path outside /s/"
    - "MUST NOT add /api/shares to the public allowlist — it is authenticated; the \"/api/\" carve-out already covers it in fallbackExcludedPrefixes"
    - "MUST NOT admit GET /api/shares/{id}/data or GET /api/shares/{id}/asset/{assetID} to isPublicShareRoute — D-10 is bearer-within-AUTH; admitting them would make every internal share anonymously readable and break SC4 row 4. They are explicit NON-members."
    - "MUST NOT add /shared/ to fallbackExcludedPrefixes — like /s/{token}, the internal page must fall through to the SPA shell; its data fetch is the gate, not the router"
    - "MUST NOT gate /api/shares/{id}/data on RequireCapability — the internal tier needs no capability (D-02); RequireAuth is its only mount gate"
---

<objective>
Mount the share surface on the parent mux: the `share.public` capability, the `/s/` public-route
predicate, and the route table.

Two traps live here, and both are the *reflex* move:

**Trap 1 — `serve_webui.go` is at 593/600.** RESEARCH's R-01 budgeted ~4 LOC for this file, but PATTERNS
re-measured honestly: the precedent comments are 3-4 lines each, so a header comment + a call is ~5 LOC →
**598/600, a 2-LOC margin**. The delta here is a hard **1-line call + a ≤2-line comment**, and everything
else goes in a new sibling file. A breach fails `make quality` pre-push **and blocks every commit**,
because the file-size hook scans the whole tree.

**Trap 2 — `/s/` must stay OUT of `fallbackExcludedPrefixes()`.** Adding a new prefix to that list is
exactly what a careful reader does next. It would be wrong: D-03 needs `/s/{token}` to fall through to
`mux.Handle("/", static)` and render the SPA shell. Adding it there 404s the public page.

Purpose: the routes exist, gated correctly, without breaking the build or the page.
Output: `cmd/aura/serve_webui_share.go` + ≤4 LOC of `serve_webui.go`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@internal/agui/share_api.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`sharePublicCapability = "share.public"`, `isPublicShareRoute`, `registerShareRoutes` (parent mux), and
the `/s/` public-route allowlist entry.

<tasks>

<task type="auto">
  <name>Task 1: serve_webui_share.go — the capability const, the public predicate, and the mount table</name>
  <read_first>
    - **RE-MEASURE FIRST:** `wc -l cmd/aura/serve_webui.go`. It was **593/600** at plan time. Your total delta to that file must keep it ≤ 598.
    - `cmd/aura/serve_webui_musr.go` — **the whole file (46 LOC). The exact template**: a header naming the phase + the "kept OUT of serve_webui.go so that file stays under the 600-LOC ceiling" reason + a per-route bullet stating **which gate and why**; a `const (…)` route block; one `register*Routes(mux, aguiHandler, auth)` func; `mux.Handle(route, aguiHandler)` for ungated and `mux.Handle(route, agui.RequireCapability(aguiHandler, auth, cap))` for gated.
    - `cmd/aura/serve_webui.go:99-118` — the capability const doc style (each const explains what it gates and **why it is not the neighbouring one**). `:118` is `governanceWriteCapability`.
    - `cmd/aura/serve_webui.go:271-278` — `identityCreateCapability` and its comment: *"the name becomes load-bearing for provisioned identities (which never get '*' nor identity.create unless the creator explicitly grants it AND holds it)"*. **This is verbatim D-02's semantics and the precedent to cite** — `share.public` is `identity.create`'s sibling, not `governance.write`'s.
    - `cmd/aura/serve_webui.go:546-559` — `isPublicPasswordResetRoute`: the predicate template — **method-checked first, then an exact-path switch, default false** (fail-closed).
    - `cmd/aura/serve_webui.go:85-97` — `fallbackExcludedPrefixes()`. **READ IT AND DO NOT EDIT IT.** Note two things: the `"/api/"` carve-out already covers `/api/shares` (an unknown share API path 404s as a backend route, correctly), and `/s/` must NOT be added or the public page 404s.
    - `cmd/aura/serve_webui.go:517` — `mux.Handle("/", static)`: the SPA catch-all that serves `/s/{token}` once `PublicRoute` admits it.
    - `internal/agui/auth.go:281-283` — `RequireCapability` and its `!SecretConfigured` pass-through (R-08). The mount gate is one of two; the other is in-handler (plan 37F-10).
    - `internal/identity/store.go:33,210` — `capNameRe = ^[a-z][a-z0-9._-]{0,63}$` and `ValidateCapabilityName`: the only gate on a capability name. Confirm `share.public` matches.
  </read_first>
  <action>
    Create `cmd/aura/serve_webui_share.go`, package `main`, modelled on `serve_webui_musr.go`.

    Header: name the phase (37F / WEBSHARE-02) and state the "kept OUT of `serve_webui.go` so that file
    stays under the 600-LOC ceiling" reason, exactly as `_musr.go` and `serve_webui.go:507-509` do. Then a
    per-route bullet stating **which gate and why**:
    - `POST /api/shares` — bare `aguiHandler`, RequireAuth-only. D-02: internal links need NO capability;
      any owner shares their own thread. The **public tier** inside the same route is gated in-handler
      (`share_api.go`) by `share.public` + the org kill-switch. Explain the split: a single route serves
      both tiers, so the tier-specific gate cannot live at the mount.
    - `GET /api/shares`, `PATCH /api/shares/{id}/snapshot`, `DELETE /api/shares/{id}` — bare `aguiHandler`;
      owner-scoped, `*ForIdentity`-gated, 404-on-foreign.
    - **`GET /api/shares/{id}/data`, `GET /api/shares/{id}/asset/{assetID}` — bare `aguiHandler`,
      `RequireAuth`-only, and deliberately NOT admitted to `PublicRoute`.** These are the D-10
      bearer-within-auth routes (plan 37F-10). State the two halves of the rule explicitly, because each
      half looks like a mistake to someone checking the other: **no capability and no owner predicate**
      (any authenticated identity holding the link resolves it — that is the whole point of the tier), but
      **authenticated** (bearer-within-**auth**; anonymous gets 401/302, SC4 row 4). They are on the
      authenticated `/api/` lane precisely so `RequireAuth` gates them for free. Do not "unify" them with
      the `/s/` routes: `isPublicShareRoute` admits every `GET /s/...` anonymously, so moving them there
      would expose every internal share to the internet.
    - `GET /s/{token}/data`, `GET /s/{token}/asset/{id}` — bare `aguiHandler` **plus** the `PublicRoute`
      admission. State loudly that these are the phase's ONLY unauthenticated routes and that the token
      predicate is their gate.

    `sharePublicCapability = "share.public"` goes in this file, following `serve_webui.go:99-118`'s doc
    style. The doc must explain what it gates and **why it is not the neighbouring capability**: cite
    `:271-277`'s `identityCreateCapability` comment as the precedent — per-user, off by default,
    admin-grantable. Explain why `governance.write` was rejected: reusing it would mean "to share your own
    chat publicly you must be a full org admin who can install MCP servers and RISKY supply-chain skills,"
    which contradicts D-02's per-user semantics and is a privilege-escalation smell.
    **Record the verified fact** (checked at plan time): the bootstrap identity is granted the literal `*`
    wildcard (`serve_bootstrap.go:176-180`), so the operator auto-holds `share.public` — intended, they are
    the admin — while **provisioned identities receive explicit named caps only**
    (`serve_onboarding.go:152-165`), so an ordinary user does not hold it unless granted. That contrast is
    what makes D-02's semantics real, and it belongs in the const's doc.

    `isPublicShareRoute(r *http.Request) bool` also goes here. Unlike `isPublicPasswordResetRoute`'s exact-
    path switch, this needs a **prefix** match: GET only, path prefixed `/s/`, **fail-closed on every other
    method and path**. This is the pure-unit-testable predicate.

    `registerShareRoutes(mux, aguiHandler, auth)` mounts the table above. Note in a comment that
    `mux.Handle` with method+path wins Go 1.22 longest-pattern precedence over the bare `"/api/"` carve-out
    and the `"/"` embed catch-all — the same note `_musr.go:47-50` carries.
  </action>
  <verify>
    <automated>go build ./... && go vet ./cmd/aura/ && golangci-lint run ./cmd/aura/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./... && go vet ./cmd/aura/` clean; `golangci-lint run ./cmd/aura/` reports 0 issues.
    - `grep -q 'sharePublicCapability = "share.public"' cmd/aura/serve_webui_share.go`.
    - `share.public` matches the capability grammar — assert by test (Task 2), not by eye.
    - The const doc cites `identity.create` as the precedent and names `governance.write` as rejected: `grep -c "identity.create\|governance.write" cmd/aura/serve_webui_share.go` returns ≥ 2.
    - The const doc records the bootstrap-`*` vs provisioned-named-caps contrast: `grep -qE "bootstrap|wildcard|\\*" cmd/aura/serve_webui_share.go`.
    - `isPublicShareRoute` is fail-closed: it checks the method first and returns false by default.
    - The internal tier is ungated at the mount: `POST /api/shares` is mounted with a bare `aguiHandler`, not wrapped in `RequireCapability`.
    - **The D-10 routes are mounted bare AND authenticated:** `GET /api/shares/{id}/data` and `GET /api/shares/{id}/asset/{assetID}` appear in the mount table wrapped in neither `RequireCapability` nor any owner predicate, and neither path appears anywhere inside `isPublicShareRoute`. `grep -c "api/shares" cmd/aura/serve_webui_share.go` is ≥ 2 in the mount table and `grep -n "api/shares" cmd/aura/serve_webui_share.go` shows **no** match inside the `isPublicShareRoute` function body.
    - The mount table's comment states the D-10 rule's two halves (no capability + no owner predicate, but authenticated) and warns against moving them onto the `/s/` lane.
    - `cmd/aura/serve_webui_share.go` ≤ 600 LOC; `bash scripts/check-file-size.sh` exits 0.
  </acceptance_criteria>
  <done>`serve_webui_share.go` holds the `share.public` const with its `identity.create`-sibling rationale and the bootstrap-`*` fact, a fail-closed `/s/` prefix predicate, and a mount table where the internal tier is ungated and the `/s/` routes are marked as the only unauthenticated surface.</done>
</task>

<task type="auto">
  <name>Task 2: serve_webui.go — one call + one chain entry, and the /s/ NON-action</name>
  <read_first>
    - **RE-MEASURE:** `wc -l cmd/aura/serve_webui.go` (593 at plan time). Budget: ≤ 598 after this task.
    - `cmd/aura/serve_webui.go:507-511` — the `registerComposerRoutes` call + its 4-line comment: the template. **Budget honestly** — PATTERNS re-measured this and found header+call ≈ 5 LOC → 598/600, a 2-LOC margin. Keep the comment to ≤2 lines.
    - `cmd/aura/serve_webui.go:523-534` — the `PublicRoute` chain, designed for exactly this: `previousPublicRoute := auth.PublicRoute` then a closure chaining `isPublicPasswordResetRoute` / `isPublicBootstrapRoute`. Add ONE `if isPublicShareRoute(r) { return true }` (2 LOC).
    - `cmd/aura/serve_webui.go:85-97` — `fallbackExcludedPrefixes()`. **READ IT. DO NOT EDIT IT.** This is the trap.
    - `internal/agui/auth.go:197,213` — `RequireAuth` and `deps.isPublicPath(p) || deps.PublicRoute(r)`: the exact allowlist hook the chain feeds.
    - `internal/agui/auth.go:167-170` — the note that the static bundle is already public ("The static bundle is the SAME code for everyone, so gating it would only break the login render without protecting anything"). This is why serving the SPA at `/s/{token}` costs no extra asset gate.
  </read_first>
  <action>
    Edit `cmd/aura/serve_webui.go` with **at most 4 LOC total**:

    1. **One `registerShareRoutes(mux, aguiHandler, auth)` call** plus a **≤2-line** comment in
       `:507-511`'s style, naming the requirement and stating that the mounts live in
       `serve_webui_share.go` to keep this file under the ceiling.
    2. **One `if isPublicShareRoute(r) { return true }`** added to the `PublicRoute` chain at `:523-534`
       (2 LOC).

    **DO NOT touch `fallbackExcludedPrefixes()` (`:85-97`).** This is an explicit non-action and the most
    likely mistake in the plan. Two facts make it so, and neither is obvious:
    - `/api/shares` is **already covered** by the `"/api/"` forward-compat carve-out, so an unknown share
      API path correctly 404s as a backend route. Nothing to add.
    - `/s/` must **stay out**: D-03 requires `/s/{token}` to fall through `mux.Handle("/", static)`
      (`:517`) to the SPA shell. Adding `/s/` to the exclusion list would make it 404 and break the public
      page. "Add the new prefix to the exclusion list" is the reflex this phase must not follow.

    If the file exceeds 598 after the edit, split something else out of it into a sibling (the precedent is
    established four times over) in the SAME commit — do not ship at the cap.

    Then create **two** test files. This is not a choice to make at execute time — `isPublicShareRoute` is
    `package main` in `serve_webui_share.go`, so a `package agui` test **cannot call it**, while
    `cmd/aura` contributes **zero** coverage at any tag. Neither file alone is sufficient, so write both:

    **(a) `cmd/aura/share_public_route_test.go`** — **no build tag.** The direct predicate test, in the
    package that can actually see the function. Implement `TestPublicShareRouteAllowlist`:
    - `GET /s/abc` ⇒ admitted; `GET /s/abc/data` ⇒ admitted; `GET /s/abc/asset/x` ⇒ admitted
    - `POST /s/abc` ⇒ **not** admitted (fail-closed on method)
    - `GET /api/shares` ⇒ **not** admitted
    - **`GET /api/shares/abc/data` ⇒ not admitted** — the D-10 internal route is authenticated
      (bearer-within-**auth**). If this case ever passes, every internal share is world-readable and SC4
      row 4 fails.
    - **`GET /api/shares/abc/asset/x` ⇒ not admitted** — same rule for the internal artifact route.
    - **`GET /shared/abc` ⇒ not admitted** — the internal-tier SPA page. `/shared/` is *visually* the
      `/s/` prefix but is not one (`"/sh"` != `"/s/"`); a naive `HasPrefix(p, "/s")` without the trailing
      slash admits it. This case proves the distinction by execution rather than by eye, and it is the
      most confusable neighbour in the route table.
    - `GET /api/conversations/1/export` ⇒ **not** admitted
    - `GET /` , `GET /login`, `GET /sabotage`, `GET /s` (no trailing slash) ⇒ **not** admitted — in
      particular `/sabotage` must not match a naive `/s` prefix
    - `TestSharePublicCapabilityNameValid` — `identity.ValidateCapabilityName("share.public")` returns no
      error (settles assumption A3 by execution rather than by inspection).

    **(b) `internal/agui/share_public_route_test.go`** — **no build tag.** The same cases exercised
    **through `agui.RequireAuth`** with an equivalent `PublicRoute` closure, asserting admitted paths
    reach the handler and refused paths get 401/302. This is the file the coverage gate measures
    (`./internal/...` only), and it tests the property that actually matters: not "does a predicate return
    true" but "does `RequireAuth` let this request through". Keep the closure's cases in lockstep with (a);
    note in both headers that they are a pair and why neither alone suffices.
  </action>
  <verify>
    <automated>bash -c 'set -e; go build ./...; go vet ./cmd/aura/ ./internal/agui/; go test ./cmd/aura/ -run "TestPublicShareRoute|TestSharePublicCapabilityName" -count=1; go test ./internal/agui/ -run "TestPublicShareRoute" -count=1; L=$(wc -l < cmd/aura/serve_webui.go); echo "serve_webui.go = $L"; [ "$L" -le 598 ] || { echo "FAIL: serve_webui.go over budget"; exit 1; }; grep -q "\"/s/\"" cmd/aura/serve_webui.go && { echo "FAIL: /s/ must NOT be in fallbackExcludedPrefixes"; exit 1; }; grep -q "\"/shared/\"" cmd/aura/serve_webui.go && { echo "FAIL: /shared/ must NOT be in fallbackExcludedPrefixes"; exit 1; }; grep -q "\"/api/shares" cmd/aura/serve_webui.go && { echo "FAIL: /api/shares must NOT be listed in serve_webui.go — the /api/ carve-out covers it"; exit 1; }; bash scripts/check-file-size.sh; echo MOUNT-OK'</automated>
  </verify>
  <acceptance_criteria>
    - `wc -l cmd/aura/serve_webui.go` returns ≤ 598 (was 593). `bash scripts/check-file-size.sh` exits 0.
    - **The NON-action holds:** `fallbackExcludedPrefixes()`'s returned slice does NOT contain `/s/`. Asserted by the automated grep and by reading the function.
    - `grep -c "registerShareRoutes" cmd/aura/serve_webui.go` returns `1`.
    - `grep -c "isPublicShareRoute" cmd/aura/serve_webui.go` returns `1` (the chain entry).
    - **BOTH test files exist** — `cmd/aura/share_public_route_test.go` (the direct predicate test, in the only package that can see `isPublicShareRoute`) and `internal/agui/share_public_route_test.go` (the same cases through `agui.RequireAuth` — the file the coverage gate measures). Neither alone satisfies this criterion.
    - `go test ./cmd/aura/ -run 'TestPublicShareRoute' -count=1` and `go test ./internal/agui/ -run 'TestPublicShareRoute' -count=1` both pass, including the `/sabotage` and `GET /s` negative cases — a naive `/s` prefix match fails these.
    - **The D-10 internal routes are proven NOT public:** `GET /api/shares/abc/data` and `GET /api/shares/abc/asset/x` are asserted **not admitted** in both files. This is SC4 row 4's unit-level counterpart.
    - **`GET /shared/abc` is asserted not admitted** — the confusable-neighbour case that a `HasPrefix(p, "/s")` without the trailing slash would fail.
    - `TestSharePublicCapabilityNameValid` passes, settling A3 by execution.
    - Both predicate test files carry **no build tag**.
    - `golangci-lint run ./cmd/aura/ ./internal/agui/` reports 0 issues.
  </acceptance_criteria>
  <done>`serve_webui.go` gained ≤4 LOC (≤598 total), `/s/` is admitted by the `PublicRoute` chain and deliberately absent from `fallbackExcludedPrefixes()`, and the fail-closed predicate is proven — in both the `cmd/aura` direct test and the coverage-measured `internal/agui` `RequireAuth` test — against `/sabotage`, bare `/s`, `/shared/abc`, the two D-10 `/api/shares/{id}/...` routes, non-GET methods, and every `/api/` path.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| whole-origin `RequireAuth` → the `/s/` carve-out | This is the only hole punched in Phase 36's whole-origin gate. Its predicate must admit exactly `/s/` GETs and nothing else — a sloppy prefix match (`/s` matching `/sabotage`) would widen the hole silently. |
| mount-level capability → loopback | `RequireCapability` vanishes when `!SecretConfigured`. The mount gate is necessary but not sufficient; plan 37F-10's in-handler kill-switch is the half that survives. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-57 | Elevation of Privilege | an over-broad public-route predicate widening the auth hole | mitigate | `isPublicShareRoute` is GET-only, `/s/`-prefixed, default false. `TestPublicShareRouteAllowlist` asserts `/sabotage`, bare `/s`, `POST /s/...`, and every `/api/` path are refused. |
| T-37F-03 | Elevation of Privilege | `share.public` gate absent on loopback (`auth.go:282`) | mitigate | Mount-level `RequireCapability` plus plan 37F-10's in-handler kill-switch (default `false`). Two gates; one survives. The mount file's doc names the split so neither is removed as redundant. |
| T-37F-58 | Elevation of Privilege | reusing `governance.write` and forcing admin rights for a user action | mitigate | Net-new `share.public`, documented as `identity.create`'s sibling; the rejected fallback and its privilege-escalation smell are recorded in the const doc and the PRD. |
| T-37F-06 | Elevation of Privilege | the `*` wildcard making capability assertions vacuous | mitigate | Verified at plan time: bootstrap grants `*` (intended — the operator is the admin); provisioned identities get named caps only. Recorded in the const doc; all capability tests seed provisioned non-wildcard identities. |
| T-37F-59 | Denial of Service | `/s/{token}` 404ing because the prefix was added to the fallback exclusion list | mitigate | Explicit non-action, stated in the plan and enforced by an automated grep gate in the verify block. |
| T-37F-60 | Denial of Service | a `serve_webui.go` LOC breach blocking every commit tree-wide | mitigate | The delta is capped at ≤4 LOC with an automated `wc -l ≤ 598` gate; everything else lives in the sibling file, per the four-times-established precedent. |
| T-37F-61 | Information Disclosure | `/api/shares` accidentally admitted to the public allowlist | mitigate | Only `isPublicShareRoute` (GET `/s/` only) is chained; `TestPublicShareRouteAllowlist` asserts `GET /api/shares` is refused. |
| T-37F-54 | Elevation of Privilege | the D-10 internal routes admitted to the public allowlist, making every internal share world-readable | mitigate | `/api/shares/{id}/data` and `/api/shares/{id}/asset/{assetID}` are explicit NON-members of `isPublicShareRoute`; both test files assert they are refused, and the mount table's comment warns against "unifying" them onto the `/s/` lane. SC4 row 4 (plan 37F-13) is the integration-level backstop. |
| T-37F-65 | Elevation of Privilege | `/shared/{id}` matching a naive `/s` prefix and joining the public allowlist | mitigate | The predicate requires the trailing slash (`/s/`); `GET /shared/abc` is an asserted negative case in both test files, alongside `/sabotage`. |
| T-37F-66 | Repudiation | the predicate tested only in `cmd/aura`, which contributes zero coverage at any tag (WR-01) | mitigate | Two files, non-conditionally: the `cmd/aura` direct predicate test plus the coverage-measured `internal/agui` test that drives the same cases through the real `RequireAuth`. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Existing deps only. |
</threat_model>

<verification>
- `go build ./... && go vet ./cmd/aura/ ./internal/agui/`
- `go test ./internal/agui/ -run 'TestPublicShareRoute|TestSharePublicCapabilityName' -count=1`
- `wc -l cmd/aura/serve_webui.go` → ≤ 598
- `fallbackExcludedPrefixes()` contains no `/s/` — grep-gated
- `golangci-lint run ./cmd/aura/ ./internal/agui/` → 0 issues
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
The share routes are mounted with the internal tier ungated and the public tier capability-gated at the
mount (backed by the in-handler kill-switch that survives loopback). `/s/{token}` is admitted by the
`PublicRoute` chain and falls through to the SPA shell because `/s/` was deliberately kept out of
`fallbackExcludedPrefixes()`. `serve_webui.go` gained ≤4 LOC and stays under the ceiling.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-12-SUMMARY.md` when done.
Record the post-edit `wc -l cmd/aura/serve_webui.go` and where the predicate test landed.
</output>
