---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 08
type: execute
wave: 4
depends_on: ["37F-04", "37F-06", "37F-07"]
files_modified:
  - internal/share/service.go
  - internal/share/bundle.go
  - internal/share/bundle_test.go
  - internal/share/service_integration_test.go
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "share.Create runs the owner gate FIRST — before minting a token, building a snapshot, or writing a byte"
    - "Only agent-produced, non-deleted, non-canceled artifacts are bundled — a user's own upload never enters a share"
    - "Artifact bytes are COPIED into token-scoped share/ blobs; the service never resolves a share's asset_id through assets.Service at read time"
    - "The default tier is internal — an absent or unknown tier never yields public"
    - "A public mint without an expiry is rejected before the DB CHECK ever sees it"
    - "Update re-snapshots to a NEW snapshot_id and keeps the token; turns added after mint never appear on the existing link until Update runs"
    - "Revoke stamps the row and drops every byte under the share's prefix"
    - "The plaintext token is returned exactly once, from Create, and never again"
    - "ResolveInternal serves ANY authenticated identity holding an internal link (D-10 bearer-within-auth) — the resolver is deliberately NOT the owner, which is the exact opposite of Create/Update/Revoke"
    - "ResolveInternal returns one indistinguishable ErrShareNotFound for a public-tier id, a revoked link, and an expired link — never a tier-mismatch signal"
  artifacts:
    - path: "internal/share/service.go"
      provides: "Create / Update / Revoke / ResolveByToken / ResolveInternal"
      min_lines: 150
    - path: "internal/share/bundle.go"
      provides: "the agent-artifacts-only filter + copy-on-share blob writer"
      min_lines: 60
  key_links:
    - from: "internal/share/service.go"
      to: "conversations GetForIdentity (via a consumer-declared seam)"
      via: "owner gate before any side effect"
      pattern: "GetForIdentity"
    - from: "internal/share/service.go ResolveInternal"
      to: "internal/share/store.go ResolveLiveByID (plan 37F-07)"
      via: "the only store method that serves a non-owner bearer read (D-10)"
      pattern: "ResolveLiveByID"
    - from: "internal/share/service.go ResolveInternal"
      to: "GET /api/shares/{id}/data (plan 37F-10, internal/agui/share_api_internal.go)"
      via: "the route is ResolveInternal's ONLY consumer — without it the internal tier mints rows no recipient can open"
      pattern: "ResolveInternal"
    - from: "internal/share/bundle.go"
      to: "objectstore.ShareArtifactKey"
      via: "copy-on-share into the token-scoped namespace"
      pattern: "ShareArtifactKey"
  prohibitions:
    - "MUST NOT bundle any artifact whose source_kind is not 'agent' — a user upload (source_kind='web', possibly a passport scan) MUST NEVER enter a share, above all a public one"
    - "MUST NOT bundle a deleted or canceled artifact"
    - "MUST NOT resolve a share's asset_id through assets.Service at recipient-read time — copy, never reference (open-webui shipped exactly this bug, granting WRITE through a share link)"
    - "MUST NOT derive the blob key from token_hash — internal-tier shares have no token"
    - "MUST NOT default an absent or unrecognized tier to public"
    - "MUST NOT mint a public link without an expires_at"
    - "MUST NOT build the share object store via IdentityStore.Resolve(ctx) — it would work by accident on an empty principal; the shared credentials are injected at the composition root, intentionally"
    - "MUST NOT import internal/agui or internal/conversations — declare consumer-side seams"
    - "MUST NOT put more than one build tag on the integration test — db_integration ONLY"
    - "MUST NOT log or return the plaintext token anywhere except the Create result"
    - "MUST NOT gate ResolveInternal on ownership — D-10 is bearer-within-auth; an owner gate breaks SC4 row 3. The asymmetry against Create/Update/Revoke is intentional and must be documented, or a reader who just absorbed owner-gate-first will 'fix' it."
    - "MUST NOT let ResolveInternal resolve a public-tier share by id — the public tier's capability is its token; an id-addressed path to a public snapshot would bypass ResolveByToken's predicate"
    - "MUST NOT re-check tier or liveness in Go inside ResolveInternal — ResolveLiveByID folds both into SQL; a Go-side re-check invites a distinguishable error and a drift between the two resolvers"
---

<objective>
Compose the primitives into the share lifecycle: `Create`, `Update`, `Revoke`, `ResolveByToken`,
`ResolveInternal` — plus the artifact bundler.

Two decisions carry the phase's sharpest security weight:
- **Agent artifacts only (D-09 amended).** The original "delivered artifacts" wording did not obviously
  forbid bundling the user's *own uploads*. It does now. Aura already encodes this rule client-side
  (`selectAgentArtifacts`); 37F enforces it at the trust boundary. Claude does exactly this: *"If you
  share a chat containing an attached file, the file itself is not included in the shared snapshot and
  remains private."*
- **Copy, never reference (D-09/R-11).** The recipient's token addresses `share/{id}/…` blobs and has no
  path to `identity/{owner}/asset/…`. open-webui shipped the opposite and granted **write** on files
  through a share link.

Purpose: the lifecycle, with the owner gate first and the leak paths structurally closed.
Output: `internal/share/service.go` + `internal/share/bundle.go`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@internal/share/store.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`share.Service`, `share.CreateRequest`, `share.CreateResult`, `share.Tier`, `share.bundleFilter`,
and the consumer-declared seams `share.ConversationReader` / `share.ArtifactLister`.

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: bundle.go — the agent-artifacts-only filter and copy-on-share</name>
  <read_first>
    - `web/src/chat/artifacts/useThreadArtifacts.ts:33-37` — `selectAgentArtifacts`: `source_kind === 'agent' && status !== 'deleted' && status !== 'canceled'`, then newest-first. **This exact predicate must be reproduced in Go, byte-for-byte the same rule.** Note the exported-pure-select rationale in its doc — it is exported specifically so it unit-tests without a render; mirror that spirit (a pure exported filter func).
    - `internal/assets/types.go:65-71` — `SourceKind{web, telegram, cli, agent}`; `SourceAgent` is the constant to compare against. Read the real constant; do not hardcode the string.
    - `internal/objectstore/types.go` — `ShareArtifactKey` / `ShareSnapshotKey` / `ShareKeyPrefix` (plan 37F-04), and the `Store` interface (`:51-58`): `Put`, `Get`, `List`, `Delete`.
    - `internal/objectstore/fake.go:17-21` — `NewFake()`: the in-memory `Store`. The bundler takes the `objectstore.Store` **interface**, which is what lets every test here run with **no Garage and no `garage_integration` tag**. This is load-bearing for the coverage floor.
    - `internal/agui/asset_service.go:11-22` — `ListForThread` + `OpenForIdentity`: the bundler's inputs. Declare a narrow consumer-side seam rather than importing agui.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §R-11 + §R-12
  </read_first>
  <behavior>
    - `source_kind == "agent"` and status is neither `deleted` nor `canceled` ⇒ bundled
    - `source_kind == "web"` ⇒ **NOT** bundled (the user's own upload)
    - `source_kind == "telegram"` ⇒ NOT bundled
    - `source_kind == "cli"` ⇒ NOT bundled
    - `source_kind == "agent"` but status `deleted` ⇒ NOT bundled
    - `source_kind == "agent"` but status `canceled` ⇒ NOT bundled
    - An unknown/empty `source_kind` ⇒ NOT bundled (fail-closed: the filter is an allowlist of one)
    - Bundling copies each artifact's bytes to `ShareArtifactKey(shareID, snapshotID, assetID)`
    - Every bundled key sits under `ShareKeyPrefix(shareID)` (so revoke's `List`+`Delete` reclaims all)
    - `dropBlobs(shareID)` lists by prefix and deletes every object; it is idempotent (a second call on an
      already-empty prefix is a no-op, not an error)
  </behavior>
  <action>
    Create `internal/share/bundle.go`, package `share`.

    Export a **pure** `BundleFilter(artifacts []ArtifactMeta) []ArtifactMeta` implementing exactly the
    `selectAgentArtifacts` predicate. Keep it pure and exported for direct unit coverage without a store,
    mirroring the TS analog's stated rationale.

    Extend `ArtifactMeta` (from plan 37F-03) — or define a bundler-local input type — to carry
    `SourceKind` and `Status` so the filter can run. `SnapshotArtifact` (the output) must still carry only
    the four allowlisted fields; the filter's inputs are not the snapshot's outputs.

    Header doc — this is the one place the rule is enforced, so say it plainly: this is **the same rule as
    `web/src/chat/artifacts/useThreadArtifacts.ts:33-37`, enforced at the trust boundary this time**.
    Cross-reference the TS site by path so the two cannot drift unnoticed. State the consequence: a
    `source_kind='web'` upload — possibly a passport scan — must never enter a share, above all a public
    one, and Claude's shipped behavior is the same. State that the filter is an **allowlist of one**
    (`agent`), so an unknown kind fails closed.

    Add `bundleArtifacts(ctx, store objectstore.Store, bucket string, shareID, snapshotID uuid.UUID,
    src ArtifactOpener, artifacts []ArtifactMeta) ([]SnapshotArtifact, error)` — copying each filtered
    artifact's bytes into the token-scoped key — and `dropBlobs(ctx, store, bucket, shareID) error`
    listing by `ShareKeyPrefix` and deleting.

    Declare `ArtifactOpener` as a **consumer-side seam** in this package (the live `assets.Service`
    satisfies it), so `internal/share` imports neither `agui` nor `assets`' HTTP layer. Follow the
    `IdentityPurger` pattern (`internal/cron/handlers/identity_purge.go:20-27`).

    Document **copy, never reference**: the recipient's token addresses `share/{id}/…` and has no path to
    `identity/{owner}/asset/…`. Name the open-webui bug (`CHANGELOG.md:331` — they granted **write**
    through a share link) as the reason this is a copy and not a lookup.

    Write `internal/share/bundle_test.go`, **no build tag**, table-driven over every `<behavior>` row.
    Use `objectstore.NewFake()` — no Garage. `TestBundleFiltersAgentArtifacts` is the name VALIDATION.md
    requires. Include `TestBundleDropBlobsIdempotent`.
  </action>
  <verify>
    <automated>go test ./internal/share/ -run 'TestBundle' -count=1 && go test -race ./internal/share/ -count=1 && go vet ./internal/share/ && golangci-lint run ./internal/share/</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -run 'TestBundle' -count=1` passes; `TestBundleFiltersAgentArtifacts` covers all 7 `<behavior>` filter rows including the fail-closed unknown kind.
    - The filter compares against the real constant, not a literal: `grep -q "assets.SourceAgent\|SourceAgent" internal/share/bundle.go`.
    - `grep -rn "garage_integration" internal/share/` returns NOTHING; the tests use `objectstore.NewFake()`.
    - `go list -deps ./internal/share/ | grep -E "internal/agui$"` returns NOTHING.
    - The header cross-references `useThreadArtifacts.ts` by path: `grep -q "useThreadArtifacts" internal/share/bundle.go`.
    - `TestBundleDropBlobsIdempotent` passes — a second `dropBlobs` on an empty prefix is a no-op.
    - Every bundled key is under the share prefix — asserted in the test via `ShareKeyPrefix`.
    - `internal/share/bundle.go` ≤ 600 LOC; `golangci-lint run ./internal/share/` reports 0 issues.
  </acceptance_criteria>
  <done>`BundleFilter` reproduces `selectAgentArtifacts` exactly in Go as an allowlist of one, `bundleArtifacts` copies bytes into token-scoped keys under the revoke prefix, `dropBlobs` is idempotent, and every test runs on `FakeStore` with no Garage tag.</done>
</task>

<task type="auto">
  <name>Task 2: service.go — Create / Update / Revoke / Resolve, owner gate first</name>
  <read_first>
    - `internal/runner/runner_delete.go:31-74` — **the analog**: the ordered-lifecycle-with-owner-gate-first shape. `:44-50` is the owner gate running BEFORE any side effect; `:31-37`'s numbered doc list matches `:51-73`'s numbered body steps; `:58-61` shows best-effort steps `slog.Warn` and continue while mandatory steps return.
    - `internal/conversations/store_identity.go:28` — `GetForIdentity`: the owner gate to call. Declare it as a consumer-side seam (`ConversationReader`), do not import `conversations`.
    - `internal/conversations/store.go:260` — `LoadHistory(ctx, convID) ([]llm.Message, error)`. **Unscoped** — the caller MUST gate with `GetForIdentity` first. That ordering is SC4 rows 1 and 2.
    - `internal/share/store.go`, `internal/share/audit.go` (plan 37F-07), `internal/share/token.go`, `internal/share/expiry.go` (37F-04), `internal/share/snapshot.go` (37F-03), `internal/share/bundle.go` (Task 1) — the pieces being composed. Read the real signatures.
    - `internal/objectstore/identity_store.go:81,151-153` — `Resolve(ctx)` reads the identity from ctx and `isShared("")` returns the SHARED creds for an empty principal. Read this to understand why the share store must be **injected at the composition root** instead: `Resolve` would return the right thing *by accident*, and an accident is not a design.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ1 (bucket + key choice) + §OQ4 (the service surface)
  </read_first>
  <action>
    Create `internal/share/service.go`, package `share`.

    `Service` holds: the `Store`, the audit writer, an `objectstore.Store` + bucket (**injected**, built
    from the shared credentials at the composition root — NOT via `IdentityStore.Resolve(ctx)`), a
    `ConversationReader` seam (`GetForIdentity` + `LoadHistory`), an `ArtifactLister`/`ArtifactOpener`
    seam, and the policy values (`maxExpiryDays`, `publicEnabled`).

    Declare every cross-package seam **consumer-side** in this package, following
    `identity_purge.go:20-27`'s `IdentityPurger` pattern, so `internal/share` imports neither
    `internal/agui` nor `internal/conversations`.

    **`Create(ctx, req CreateRequest) (CreateResult, error)`** — numbered steps in the doc comment
    matching numbered steps in the body (the `runner_delete.go` discipline):
    1. **Owner gate FIRST** — `conv.GetForIdentity(ctx, convID, owner)`; a miss returns
       `ErrShareNotFound` so the surface 404s. **Nothing** is minted, built, or written before this. This
       is SC4 rows 1 and 2.
    2. Resolve the tier. **Absent or unrecognized ⇒ internal.** Public is only ever reached by an explicit
       `public` value (D-01: never default).
    3. If public: check `publicEnabled` (the org kill-switch) → deny; then require an expiry via
       `ResolveExpiry` (rejecting ≤0, clamping above the cap). The capability check lives at the handler
       (plan 37F-10) because it needs the request principal; document that split here so the two gates read
       as intentional.
    4. Load history (`LoadHistory`) and list artifacts; `BundleFilter` them.
    5. `BuildSnapshot(...)` → the redacted Snapshot. Marshal via `Snapshot.JSON()`.
    6. Mint a new `snapshot_id`; `Put` the canonical JSON at `ShareSnapshotKey(shareID, snapshotID)`;
       `bundleArtifacts` the filtered artifacts.
    7. If public: `Mint()` the token; store `Hash(plaintext)`. Internal: no token.
    8. `Insert` the row. Audit `create`.
    9. Return `CreateResult` carrying the **plaintext token exactly once** (public only).

    **`Update(ctx, shareID, identityID)`** — owner gate first, re-snapshot to a **new** `snapshot_id`
    (never overwrite the live blob mid-write — that is why the key carries `snapshot_id`), swap the row
    pointer, bump `updated_at`, drop the *old* snapshot's blobs, audit `update`. **Keep the token** (D-06
    "Update" semantics — Claude's "unshare and share again" yields a new URL; open-webui's keep-the-URL
    semantic is what D-06 words).

    **`Revoke(ctx, shareID, identityID)`** — owner gate first; **drop blobs, then stamp `revoked_at`**.
    Document the ordering: a crash between the two re-runs the idempotent delete on the next call;
    stamp-then-delete would orphan bytes permanently (R-10).

    **`ResolveByToken(ctx, plaintext string) (Snapshot, Link, error)`** — `Hash` the plaintext, resolve
    via the store's lazy predicate, `Get` the snapshot blob, unmarshal, return. Audit `open` with **no
    recipient PII**. Any error ⇒ `ErrShareNotFound` (no oracle).

    **`ResolveInternal(ctx, shareID, identityID) (Snapshot, Link, error)`** — D-10 bearer-within-auth:
    **any** authenticated identity holding the link may open the already-redacted snapshot. This is the
    resolver behind `GET /api/shares/{id}/data` (plan 37F-10), which is its **only** consumer; without
    that route the internal tier — D-01's DEFAULT share action — mints rows no recipient can ever open.

    Steps: call `Store.ResolveLiveByID(ctx, shareID)` (plan 37F-07). That is the **only** store method
    that can serve this read — `GetForIdentity` carries an owner filter and would always miss for a
    bearer, which is the whole reason `ResolveLiveByID` exists. Its SQL predicate already enforces
    `tier='internal'` + `revoked_at IS NULL` + not-expired, so an unknown id, a public-tier id, a revoked
    link, and an expired link all arrive here as the **same** miss. Map every miss to `ErrShareNotFound`
    and do **not** re-check tier or liveness in Go — a Go-side re-check is how a distinguishable error
    (and a drift between the two resolvers) gets introduced. Then `Get` the snapshot blob and unmarshal,
    exactly as `ResolveByToken` does.

    **Document the asymmetry, loudly.** `Create`/`Update`/`Revoke` run an owner gate FIRST; this method
    deliberately runs **none**. A reader who has just absorbed the owner-gate-first discipline three
    functions above will read the absence as a bug and "fix" it — which would silently reduce the internal
    tier to an owner-only view. State that D-10 makes the *link* the capability and `RequireAuth` (the
    route mount, plan 37F-12) the gate; that the snapshot is redacted (D-08), so a bearer never sees more
    than the owner chose to share; and that this is **SC4 row 3** — the one row where the expected answer
    is 200 rather than 404.

    Audit `open` with the resolving `identityID`. Unlike `ResolveByToken`'s anonymous open — where the
    audit must carry **no** recipient PII (D-14) — the internal resolver has a first-class Aura identity
    to record, and there is no PII concern in recording it. Recording it is what makes "authenticated and
    auditable" true rather than aspirational, and that claim is load-bearing: it is a stated pillar of the
    **OQ#4 resolution** recorded in the PRD (plan 37F-01), which is what justifies addressing an internal
    share by id at all.

    Error discipline: `%w` everywhere, prefix `"<operation>: "`. **Never `%w` a token.**

    If `service.go` approaches 600 LOC, split by concern (`service_create.go` / `service_resolve.go`) in
    the SAME commit — do not ship at the cap.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/share/ && golangci-lint run ./internal/share/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./... && go vet ./internal/share/` clean; `golangci-lint run ./internal/share/` reports 0 issues.
    - **Owner gate is first:** in `Create`, the `GetForIdentity` call appears at a lower line number than any `Mint`, `Put`, `Insert`, or `BuildSnapshot` call. Verified by `grep -n` ordering within the function.
    - `go list -deps ./internal/share/ | grep -E "internal/(agui|conversations)$"` returns NOTHING.
    - `grep -n "IdentityStore\|\.Resolve(ctx)" internal/share/service.go` returns NOTHING — the object store is injected.
    - Tier defaulting is fail-closed: the tier switch has an explicit `public` case and a `default:` that yields internal; no code path assigns public without an explicit request value.
    - Revoke order: within `Revoke`, the `dropBlobs` call appears at a lower line number than the `RevokeForIdentity` (stamp) call.
    - `grep -nE "%w.*[Tt]oken|Errorf.*plaintext" internal/share/service.go` returns NOTHING.
    - **`ResolveInternal` has no owner gate:** its function body contains no `GetForIdentity` call, and a doc comment within 5 lines of its signature states the omission is D-10-intended and contrasts it with `Create`'s owner-gate-first rule.
    - **`ResolveInternal` delegates tier + liveness to the store:** its body calls `ResolveLiveByID` and contains no Go-side `tier ==` / `Tier ==` comparison and no `revoked`/`expires` comparison.
    - **`ResolveInternal` maps every store miss to `ErrShareNotFound`:** no code path returns a distinct error for a public-tier id, a revoked link, or an expired link.
    - **`ResolveInternal` audits `open` with the resolving identity** — the audit call names `identityID`, unlike `ResolveByToken`'s PII-free open.
    - The doc comment carries numbered steps matching the numbered body steps (the `runner_delete.go` discipline).
    - Every file ≤600 LOC; `bash scripts/check-file-size.sh` exits 0.
  </acceptance_criteria>
  <done>`Service` composes the lifecycle with the owner gate before every side effect, a fail-closed tier default, drop-blobs-then-stamp revoke, snapshot-id-keyed Update that keeps the token, and no import of `agui`/`conversations`.</done>
</task>

<task type="auto">
  <name>Task 3: service integration tests — frozen snapshot, update, revoke-drops-blobs, default tier</name>
  <read_first>
    - `internal/agui/auth_capability_integration_test.go` — the coverage-gate-safe test shape (single tag, header, `migratedPool(t)`, `t.Cleanup`, fresh non-wildcard identity seeded with `name = "..."+t.Name()`)
    - `internal/share/store_integration_test.go` — plan 37F-07's harness; reuse its seeding helpers rather than duplicating them
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the exact names: `TestShareSnapshotFrozen`, `TestShareUpdateResnapshot`, `TestShareRevokeDropsBlobs`, `TestDefaultTierIsInternal`, `TestShareBundledArtifactTokenScoped`
  </read_first>
  <action>
    Create `internal/share/service_integration_test.go` with the **single** build tag `db_integration`,
    using `objectstore.NewFake()` (no Garage, no extra tag).

    Tests:
    - `TestDefaultTierIsInternal` — a `CreateRequest` with an absent tier yields `internal`; one with a
      garbage tier also yields `internal`, never public. (VALIDATION lists this as a unit test; it is
      cheaper as one — if the tier resolution is extractable as a pure func, put this case in
      `bundle_test.go`/a unit file with **no tag** instead, and say so in the SUMMARY. Prefer the untagged
      home: an untagged test runs everywhere.)
    - `TestShareSnapshotFrozen` — **the D-06 core**: mint a link, append a new turn to the conversation,
      resolve the link again, assert the new turn is absent. This is SC3's leak-prevention.
    - `TestShareUpdateResnapshot` — Update produces a NEW `snapshot_id`, bumps `updated_at`, keeps the same
      token (resolve with the original plaintext still works), and the new turn now appears.
    - `TestShareRevokeDropsBlobs` — after Revoke, `FakeStore.List(ShareKeyPrefix(shareID))` is empty AND
      `ResolveByToken` returns `ErrShareNotFound`.
    - `TestShareBundledArtifactTokenScoped` — a bundled artifact is readable at
      `ShareArtifactKey(shareID, snapshotID, assetID)` and the service never calls the identity-scoped
      opener at resolve time (copy, not reference).
    - `TestShareResolveInternalBearer` — **the D-10 core.** A mints an internal link; **B** — a different
      provisioned identity who is NOT the owner — calls `ResolveInternal` and gets the snapshot. This is
      SC4 row 3 at the service layer, and it is the test that fails the moment anyone adds an owner filter
      to `ResolveLiveByID` or an owner gate to `ResolveInternal`. Resolving as A would pass vacuously —
      the test MUST resolve as B.
    - `TestShareResolveInternalRejectsPublicTier` — a **public** share's id passed to `ResolveInternal`
      returns `ErrShareNotFound`, never the snapshot: the public tier is token-addressed only.
    - `TestShareResolveInternalLazyLiveness` — a revoked internal link and an expired internal link both
      return `ErrShareNotFound` from `ResolveInternal` **with the sweep never run**, indistinguishable
      from the unknown-id case.
    - `TestSharePublicRequiresExpiryService` — a public `CreateRequest` with no expiry is rejected by the
      **service**, before the DB CHECK. Both layers must refuse it; this test proves the Go layer does.
    - `TestSharePublicDeniedWhenKillSwitchOff` — `publicEnabled=false` ⇒ a public Create is denied even
      when everything else is valid.

    Seed **provisioned non-wildcard identities** (R-13). Use the shared `envOrSkip` (it `t.Fatal`s under
    `$CI`). **Exactly one build tag.**
  </action>
  <verify>
    <automated>go test -tags db_integration -race -p 1 -count=1 ./internal/share/ && go test ./internal/share/ -count=1 && go test -tags db_integration -cover -p 1 -count=1 ./internal/share/</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -race -p 1 -count=1 ./internal/share/` passes with a **non-sub-second** runtime.
    - The untagged suite still passes: `go test ./internal/share/ -count=1`.
    - `head -1 internal/share/service_integration_test.go` is exactly `//go:build db_integration`.
    - `grep -rn "garage_integration\|neo4j_integration\|authula_integration\|musr_e2e\|docker_integration" internal/share/` returns NOTHING.
    - `TestShareSnapshotFrozen` appends a real turn between mint and resolve and asserts its absence — the frozen-snapshot property is proven, not assumed.
    - `TestShareRevokeDropsBlobs` asserts BOTH an empty prefix listing AND a 404-class resolve.
    - `TestShareUpdateResnapshot` asserts the token is unchanged (resolve with the original plaintext succeeds after Update).
    - **`TestShareResolveInternalBearer` resolves as B, the NON-owner**, and asserts success — the D-10 property. A test that resolves as the owner passes vacuously and does not satisfy this criterion.
    - `TestShareResolveInternalRejectsPublicTier` and `TestShareResolveInternalLazyLiveness` assert `errors.Is(err, ErrShareNotFound)` for the public-tier, revoked, and expired cases — one sentinel, no tier oracle.
    - `internal/share` package coverage under `db_integration` is ≥ 85%: `go test -tags db_integration -cover -p 1 ./internal/share/` reports ≥ 85.0%.
    - No test grants `*` to a seeded identity.
  </acceptance_criteria>
  <done>The frozen-snapshot property, token-preserving Update, revoke-drops-blobs, the fail-closed tier default, the kill-switch denial, and token-scoped artifact copies are each proven against a real Postgres with `FakeStore`, under a single `db_integration` tag, at ≥85% package coverage.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| requester → another identity's conversation | `Create`'s owner gate is the boundary. It runs before any side effect, so a foreign convID cannot even cause a blob write. |
| owner's private uploads → a share | `BundleFilter` is the boundary. `source_kind='web'` is the user's own file; it must never cross. |
| identity-scoped asset lane → recipient | The copy is the boundary. A recipient's token addresses `share/…` only and has no route into `identity/…`. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-09 | Information Disclosure | a user's own upload (e.g. a passport scan) bundled into a public link | mitigate | `BundleFilter` is an allowlist of one (`source_kind == agent`), enforced server-side, mirroring `selectAgentArtifacts` and Claude's shipped rule. Unknown kinds fail closed. Table-tested per kind. |
| T-37F-08 | Tampering | write-through-share (open-webui's shipped bug) | mitigate | Copy-on-share: bytes are `Put` into `share/{id}/…` at mint; the resolve path never calls the identity-scoped opener. Proven by `TestShareBundledArtifactTokenScoped`. |
| T-37F-41 | Elevation of Privilege | minting a link to another identity's conversation | mitigate | Owner gate first in `Create`, asserted by line-order in the acceptance criteria and by SC4 row 2 (plan 37F-13). |
| T-37F-42 | Information Disclosure | turns added after mint leaking onto an existing link | mitigate | Static snapshot: the blob is written once at mint and only replaced by an explicit `Update`. `TestShareSnapshotFrozen` appends a turn and asserts absence. |
| T-37F-43 | Elevation of Privilege | public tier reached by default or by a typo'd tier value | mitigate | The tier switch's `default:` yields internal; public requires an explicit value. `TestDefaultTierIsInternal` covers absent and garbage. |
| T-37F-27 | Information Disclosure | a public link with no expiry | mitigate | Refused twice: by the service (`TestSharePublicRequiresExpiryService`) and by the DB `shared_links_tier_shape` CHECK (plan 37F-07's SQLSTATE test). |
| T-37F-07 | Information Disclosure | Garage bytes surviving revoke | mitigate | `Revoke` drops blobs BEFORE stamping the row, so a crash re-runs the idempotent delete; stamp-then-delete would orphan bytes permanently (R-10). Line-order asserted. |
| T-37F-44 | Information Disclosure | the share store silently resolving the wrong credentials | mitigate | The object store is injected at the composition root from the shared credentials, never via `IdentityStore.Resolve(ctx)` — which would return the right thing by accident on an empty principal. Grep-gated. |
| T-37F-11 | Information Disclosure | plaintext token leaking past Create | mitigate | Returned only in `CreateResult`; never stored, logged, or `%w`-wrapped. Grep-gated. |
| T-37F-45 | Elevation of Privilege | an owner gate/filter added to the internal resolver, silently reducing D-10 to owner-only | mitigate | `ResolveInternal` runs no owner gate and `ResolveLiveByID` takes no owner argument — both documented as intended, against the owner-gate-first discipline three functions above. `TestShareResolveInternalBearer` resolves as a NON-owner and fails the moment a filter appears. |
| T-37F-46 | Information Disclosure | a public share resolved by id on the authenticated route, bypassing the token predicate | mitigate | `tier='internal'` is folded into `ResolveLiveByID`'s SQL, so a public row never reaches Go; `ResolveInternal` adds no Go-side tier check that could leak a mismatch. `TestShareResolveInternalRejectsPublicTier` asserts `ErrShareNotFound`. |
| T-37F-53 | Information Disclosure | an expired/revoked internal link resolving because liveness was only swept, not enforced lazily | mitigate | `ResolveLiveByID` carries the same DB-clock lazy predicate as `ResolveByToken`; `TestShareResolveInternalLazyLiveness` proves it with the sweep never run. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Existing deps only. |
</threat_model>

<verification>
- `go build ./... && go vet ./internal/share/`
- `go test ./internal/share/ -count=1` (untagged)
- `go test -tags db_integration -race -p 1 -count=1 ./internal/share/`
- `go test -tags db_integration -cover -p 1 ./internal/share/` → ≥ 85%
- `golangci-lint run ./internal/share/` → 0 issues
- `bash scripts/check-file-size.sh` → 0
- Gate on a **disposable** DB only (`bash scripts/coverage_docker.sh`), never the live `aura` DB.
</verification>

<success_criteria>
The share lifecycle runs the owner gate before any side effect, bundles agent-produced artifacts only
(a user upload can never enter a share), copies bytes into a token-scoped namespace the recipient's
token cannot escape, defaults fail-closed to the internal tier, refuses a public mint without an expiry
at both the Go and DB layers, keeps snapshots frozen until an explicit Update, and drops every byte on
revoke before stamping the row.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-08-SUMMARY.md` when done.
Record whether `TestDefaultTierIsInternal` landed untagged (preferred) or tagged, and the coverage number.
</output>
