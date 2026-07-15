---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 05
type: execute
wave: 2
depends_on: ["37F-01"]
files_modified:
  - web/src/chat/artifacts/renderers/assetSourceContext.ts
  - web/src/chat/artifacts/renderers/useAssetContent.ts
  - web/src/chat/artifacts/PreviewModal.tsx
  - web/src/chat/share/shareTypes.ts
  - web/src/i18n/resources.share.ts
  - web/src/i18n/resources.ts
  - web/src/chat/artifacts/renderers/assetSourceContext.test.ts
  - web/src/i18n/resources.share.test.ts
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "Every existing artifact renderer keeps fetching /api/assets/{id}/download with credentials same-origin when no provider is mounted — the default is byte-identical to today's behavior"
    - "A subtree wrapped in an AssetSourceContext provider resolves artifact bytes through the provider's URL instead, with no edit to HtmlPreview or any other renderer"
    - "The public path sends no credentials — a recipient has no session and cookies must not go to an unauthenticated route"
    - "Every share i18n key exists in BOTH en and it"
    - "The TypeScript Snapshot type mirrors the Go wire contract key-for-key"
  artifacts:
    - path: "web/src/chat/artifacts/renderers/assetSourceContext.ts"
      provides: "AssetSourceContext + useAssetSource with an identity-scoped default (R-05)"
      exports: ["AssetSourceContext", "useAssetSource"]
    - path: "web/src/i18n/resources.share.ts"
      provides: "shareEn / shareIt under one top-level share namespace"
      exports: ["shareEn", "shareIt"]
    - path: "web/src/chat/share/shareTypes.ts"
      provides: "TS mirror of the Go Snapshot wire contract"
  key_links:
    - from: "web/src/chat/artifacts/renderers/useAssetContent.ts"
      to: "web/src/chat/artifacts/renderers/assetSourceContext.ts"
      via: "useAssetSource() supplies the URL + credentials"
      pattern: "useAssetSource"
    - from: "web/src/i18n/resources.ts"
      to: "web/src/i18n/resources.share.ts"
      via: "import + spread in both language blocks"
      pattern: "shareEn|shareIt"
  prohibitions:
    - "MUST NOT inline share i18n keys into resources.ts — it is at 576/600 and that is the R-03 breach"
    - "MUST NOT put the context object and a component in the same module — react-refresh/only-export-components would fail the web lint gate; the context lives in a .ts module"
    - "MUST NOT thread an assetUrl prop through the renderer chain — context-with-a-default is the extract-a-helper answer; prop-threading churns 6 lazy chunks and every existing test"
    - "MUST NOT change the default behavior of any existing renderer call site — the default context value must reproduce today's URL and credentials exactly"
    - "MUST NOT fork HtmlPreview for the public page — it must work unedited once useAssetContent reads the context"
    - "MUST NOT send credentials on the public asset path"
    - "MUST NOT persist the owner's language into the snapshot — the public page falls back to Accept-Language"
---

<objective>
Land the three web foundations every later 37F web plan depends on: the asset-source seam (R-05), the
share i18n module (R-03), and the TypeScript mirror of the Go snapshot wire contract.

R-05 is the blocker that makes the public page possible at all: the 37B renderers **hardcode** the
identity-scoped asset URL (`useAssetContent.ts:32`, `PreviewModal.tsx:73,101`), so the public page
cannot reuse them as D-03 mandates. The fix is a context with a default that reproduces today's
behavior exactly — every existing call site and test stays byte-identical, and `HtmlPreview` works on
the public page with **zero edits**. That payoff is the whole argument against prop-threading.

Purpose: unblock the web surface; define the contracts before the consumers.
Output: `assetSourceContext.ts`, `resources.share.ts`, `shareTypes.ts`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@CLAUDE.md
</context>

## Artifacts this plan produces

`AssetSourceContext`, `useAssetSource`, `AssetSource` (type), `shareEn`, `shareIt`, and the TS
`Snapshot` / `SnapshotTurn` / `SnapshotArtifact` / `ShareLink` mirror.

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: AssetSourceContext — the R-05 seam, with an identity-scoped default</name>
  <read_first>
    - `web/src/chat/voice/voiceModeContext.ts` — **the whole file (35 LOC). This is the exact template**: context + hook in a NON-component `.ts` module, a safe default constant so no consumer throws without a provider, and a one-line hook doc naming the default behavior.
    - `web/src/chat/artifacts/renderers/useAssetContent.ts` — the whole file. Its header already states the extract-a-helper rationale ("Extracted so the four non-object-URL renderers don't duplicate the fetch+abort block"); `:32-36` is the hardcoded fetch to re-point.
    - `web/src/chat/artifacts/PreviewModal.tsx:73,101` — two more hardcoded `/api/assets/{id}/download` hrefs
    - `web/src/chat/artifacts/renderers/HtmlPreview.tsx` — the whole file. **Do not edit it.** Read it to verify for yourself that once `useAssetContent` reads the context, this file works on the public page unchanged. That is the acceptance test for the seam's design.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §R-05 — the mitigation and the "drop credentials on the public path" note
  </read_first>
  <behavior>
    - No provider mounted, `useAssetSource().assetUrl(id)` returns `/api/assets/<encoded id>/download` and `.credentials` is `same-origin` (byte-identical to today)
    - A provider supplying a token-scoped resolver, `assetUrl(id)` returns that URL and `credentials` is `omit`
    - `assetUrl` percent-encodes the asset id (an id is never interpolated raw)
    - `useAssetContent` fetches whatever `useAssetSource()` resolves, with the resolved credentials
    - Existing renderer tests pass unchanged (the default preserves current behavior)
  </behavior>
  <action>
    Create `web/src/chat/artifacts/renderers/assetSourceContext.ts` — a **non-component `.ts` module**.
    A `.tsx` exporting both a context and a component would fail the `react-refresh/only-export-components`
    lint rule and therefore `make quality`'s web gate; `voiceModeContext.ts` exists for exactly this
    reason and says so in its header. Mirror that.

    Export an `AssetSource` interface with an `assetUrl(assetId: string): string` member and a
    `credentials: RequestCredentials` member. Define an `IDENTITY_SCOPED` default constant whose
    `assetUrl` returns the `/api/assets/<encodeURIComponent(assetId)>/download` path and whose
    `credentials` is `same-origin`. Export `AssetSourceContext` built with `createContext` defaulted to
    `IDENTITY_SCOPED`, and a `useAssetSource()` hook over `useContext`.

    Header doc — state the three non-obvious WHYs:
    - The default is the identity-scoped lane **so every existing call site and test stays
      byte-identical**; the public share page wraps its tree in a provider returning the token-scoped URL,
      which is what lets `HtmlPreview` and the other renderers work on the public page with zero edits
      (D-03/D-09).
    - The alternative and why it lost: threading an `assetUrl` prop through six lazy renderer chunks is
      churn across every call site and test; context-with-a-default is the "extract a helper, never
      duplicate" answer.
    - `credentials` lives on the context value rather than hardcoded because the recipient has no session
      and sending cookies to an unauthenticated route is needless (R-05).

    Re-point the three call sites — `useAssetContent.ts:32-36` and `PreviewModal.tsx:73,101` — to read
    `useAssetSource()`. **Do not change their behavior under the default.** `PreviewModal`'s two sites are
    `href` attributes on links, not fetches, so they take `assetUrl(id)` only.

    Refactor-on-touch (CLAUDE.md): while in `useAssetContent.ts` and `PreviewModal.tsx`, remove dead code,
    fold duplication, keep each ≤600 LOC, and update any comment the change makes stale — in the SAME
    commit.

    Write `assetSourceContext.test.ts` (vitest) covering every `<behavior>` row, including the
    no-provider default. Run the existing renderer tests to prove they pass unchanged.

    Web gates run on **Windows Git Bash**, not WSL (WSL has no node).
  </action>
  <verify>
    <automated>npx vitest run web/src/chat/artifacts && npx tsc --noEmit -p web/tsconfig.json</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/chat/artifacts` passes, including the **pre-existing** renderer tests with no edits to them: `git diff --name-only` lists no `web/src/chat/artifacts/renderers/*.test.tsx` other than the new `assetSourceContext.test.ts`.
    - `web/src/chat/artifacts/renderers/HtmlPreview.tsx` is UNCHANGED: `git diff --name-only` does not list it. This is the seam's whole design goal.
    - `assetSourceContext.ts` has a `.ts` extension and exports no React component: `grep -cE "return \(|<[A-Z]" web/src/chat/artifacts/renderers/assetSourceContext.ts` returns `0`.
    - `grep -c "/api/assets/" web/src/chat/artifacts/renderers/assetSourceContext.ts` returns ≥1 (the default), and `grep -rn "/api/assets/.*/download" web/src/chat/artifacts/renderers/useAssetContent.ts web/src/chat/artifacts/PreviewModal.tsx` returns NOTHING (all three sites re-pointed).
    - `grep -q "encodeURIComponent" web/src/chat/artifacts/renderers/assetSourceContext.ts` succeeds.
    - `npx tsc --noEmit -p web/tsconfig.json` is clean.
    - `npx eslint web/src/chat/artifacts/renderers/assetSourceContext.ts` reports 0 errors (proves the `only-export-components` rule is satisfied).
    - All touched files ≤600 LOC.
  </acceptance_criteria>
  <done>`AssetSourceContext` exists in a non-component module with an identity-scoped default; all three hardcoded URL sites read it; `HtmlPreview` is untouched and every existing renderer test passes unedited.</done>
</task>

<task type="auto">
  <name>Task 2: shareTypes.ts — the TypeScript mirror of the Go snapshot wire contract</name>
  <read_first>
    - `internal/share/snapshot.go` — the Go `Snapshot` / `SnapshotTurn` / `SnapshotArtifact` json tags (built in plan 37F-03). **These tags ARE the contract.** Read them from the file, not from this plan.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ4 — the same shape, as specified
    - `web/src/chat/artifacts/artifactMeta.ts` — an existing small web type/util module; follow its export + doc style
  </read_first>
  <action>
    Create `web/src/chat/share/shareTypes.ts` mirroring the Go `Snapshot` wire contract exactly. Fields:
    `schema_version` (number), `title`, `model`, `created_at`, `snapshot_at` (strings), `turns`
    (SnapshotTurn array), `artifacts` (SnapshotArtifact array). `SnapshotTurn` carries `seq` (number),
    `role`, `text`, and an optional `tool_names` (string array). `SnapshotArtifact` carries `asset_id`,
    `filename`, `mime_type` (strings) and `size_bytes` (number).

    Type `role` as the literal union of `user` and `assistant` only. The Go projection guarantees no other
    role reaches the wire, so widening it to `string` here would invite a renderer branch for a case that
    cannot occur.

    Also declare the share-link view type the modal and the management surfaces consume — `ShareLink`
    with `id`, `tier` (the literal union of `internal` and `public`), optional `url`, optional
    `expires_at`, optional `revoked_at`, `created_at`, `updated_at`. This is the API contract plan
    37F-10 serves.

    Header doc: state that this file mirrors `internal/share/snapshot.go`'s json tags and that the two
    must not drift; name the Go file so a reader can diff them. State that no field capable of carrying
    tool arguments, results, or a path exists on either side — the TS type is the second statement of the
    same invariant, not an independent design.

    Do **not** re-derive the shape from imagination. If `internal/share/snapshot.go` disagrees with
    RESEARCH §OQ4, **the Go file wins** — it is the serializer — and the SUMMARY records the discrepancy.
  </action>
  <verify>
    <automated>npx tsc --noEmit -p web/tsconfig.json && node scripts/check_share_wire_contract.cjs</automated>
  </verify>
  <acceptance_criteria>
    - `npx tsc --noEmit -p web/tsconfig.json` is clean.
    - **Wire-contract parity is machine-checked, not eyeballed:** write `scripts/check_share_wire_contract.cjs`, a small node script that extracts every `json:"<tag>"` from `internal/share/snapshot.go` and asserts each tag string appears in `web/src/chat/share/shareTypes.ts`, exiting non-zero on any missing tag. It prints the mirrored tag count on success. Running it exits 0.
    - `grep -cE "'user' \| 'assistant'|\"user\" \| \"assistant\"" web/src/chat/share/shareTypes.ts` returns ≥1 — `role` is a literal union, not `string`.
    - `grep -nE "\bpath\b|arguments|args|result_preview|sidecar|identity_id|tool_call_id" web/src/chat/share/shareTypes.ts` returns NOTHING — the TS mirror carries no leak-capable field.
    - The header names `internal/share/snapshot.go` explicitly.
    - `web/src/chat/share/shareTypes.ts` ≤600 LOC.
  </acceptance_criteria>
  <done>`shareTypes.ts` mirrors the Go snapshot json tags exactly (proven by a checked-in parity script), types `role` as a literal union, carries no leak-capable field, and declares the `ShareLink` API view type.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: resources.share.ts — the share i18n module (R-03), en + it</name>
  <read_first>
    - `web/src/i18n/resources.compaction.ts` — **the whole file (64 LOC), the closest size match**: two exports `<domain>En`/`<domain>It`, one top-level namespace key, identical key trees, `{{interpolation}}`, nested sub-objects for enumerations, proper Italian typography (right single quote U+2019, ellipsis U+2026)
    - `web/src/i18n/resources.ts:1-17` — the per-domain import block; and the two spread sites (around `:160` for en and `:437` for it). **RE-MEASURE `wc -l web/src/i18n/resources.ts` first** — it was 576/600 at plan time; the delta here must be ~4 LOC.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"UI/UX Research — Share Surface" §2 (the modal copy, including the ChatGPT honesty note), §3 (the management surface copy), §4 (the public page), §5 (revoke/expiry UX), §6 (accessibility + i18n)
  </read_first>
  <action>
    Create `web/src/i18n/resources.share.ts` exporting `shareEn` and `shareIt`, each with ONE top-level
    `share` namespace key so `t('share.modal.title')` resolves.

    Cover every string the later web plans need, grouped into sub-objects: the toggle aria-label; the
    modal (title, the tier fieldset legend, the internal + public option labels and their descriptions,
    the public warning, the expiry chips 1d/7d/30d/custom, the snapshot-frozen note, cancel/create);
    the shared state (the URL label, copy/copied, the tier+expiry+created metadata line, the stale-snapshot
    note with a `{{count}}` interpolation, update, revoke); the revoke confirm (title, body, confirm
    label); the "Condiviso" section (heading, empty state); the settings management surface (heading,
    empty state, the tier badges, the expires-in and expired labels, revoke, revoke-all + its confirm);
    and the public page (the snapshot-date line, the discreet "shared from Aura" mark, the 404 body).

    Copy guidance from RESEARCH §UI/UX 2, which is the design source:
    - The public warning renders **only when public is selected** — a warning shown always is a warning
      nobody reads.
    - Include the ChatGPT honesty note **at mint time**, not only at revoke: revoking prevents new access
      but does not delete copies already seen or cached by search engines. Mint is when the decision is
      made.
    - The stale-snapshot line is Aura's differentiator: "N new messages are not in this link" — the data
      already exists (`conversations.last_active_at` vs `shared_links.updated_at`), so it costs no storage.
    - Expiry displays relative ("expires in 6 days") with the absolute date on the title/tooltip.

    Italian is a first-class language here, not a translation afterthought: use proper typography (U+2019
    apostrophe, U+2026 ellipsis) as `resources.compaction.ts:44` does.

    **Do NOT persist the owner's language into the snapshot** — the public page falls back to the browser
    `Accept-Language` via the i18next detector default. Persisting it is fingerprinting-adjacent and a
    needless coupling (RESEARCH §6).

    Wire into `resources.ts` with **exactly 4 LOC**: one import + one spread in each language block.
    Do not inline the keys — that is the R-03 breach.

    Write `web/src/i18n/resources.share.test.ts` asserting **key-tree parity**: every leaf key path in
    `shareEn` exists in `shareIt` and vice versa. Walk the trees recursively — a flat key-count comparison
    would pass two trees with the same size and different keys.

    Note for the reviewer, in the module header: all **prompt/LLM-facing** overlays are English-only on
    this project, but these are **user-facing UI strings**, which are en+it by the i18n rule. The two
    rules do not conflict; say so, because it looks like they might.
  </action>
  <verify>
    <automated>npx vitest run web/src/i18n && npx tsc --noEmit -p web/tsconfig.json && test "$(wc -l < web/src/i18n/resources.ts)" -le 582 && echo I18N-OK</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/i18n` passes, including the new recursive key-parity test.
    - The parity test walks the tree recursively (asserts on leaf key PATHS, not on a count): `grep -qiE "recurs|walk|flatten|paths" web/src/i18n/resources.share.test.ts`.
    - `wc -l web/src/i18n/resources.ts` returns ≤ 582 (was 576; the delta is ~4 LOC for one import + two spreads).
    - `web/src/i18n/resources.share.ts` exports both `shareEn` and `shareIt`: `grep -c "export const share\(En\|It\)" web/src/i18n/resources.share.ts` returns `2`.
    - Both exports are wired: `grep -c "shareEn\|shareIt" web/src/i18n/resources.ts` returns ≥3 (one import naming both, one spread each).
    - Each export has exactly one top-level namespace key (`share`).
    - The Italian tree uses U+2019 and U+2026 where apostrophes/ellipses occur (spot-check ≥1 each).
    - The public warning copy includes the cache/search-engine honesty note.
    - `bash scripts/check-file-size.sh` exits 0.
  </acceptance_criteria>
  <done>`resources.share.ts` carries every share string in both en and it under one namespace, wired into `resources.ts` in 4 LOC (≤582 total), with recursive key-tree parity proven by test.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| authenticated SPA subtree → public SPA subtree | The same renderer components run in both. The context value is what distinguishes them; a default that sent credentials on the public path would leak the owner's session cookie to an unauthenticated route. |
| Go serializer → TS renderer | The json tags are a contract across two languages with no compiler linking them. Drift is invisible at build time, so it is asserted by a script. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-28 | Information Disclosure | cookies sent to the unauthenticated `/s/{token}` asset path | mitigate | `credentials` is a context value, not a hardcode; the public provider supplies `omit` (R-05). |
| T-37F-29 | Elevation of Privilege | public page reaching the identity-scoped asset lane | mitigate | The public provider returns only token-scoped URLs; no renderer constructs `/api/assets/...` itself once the three sites are re-pointed (grep-gated). |
| T-37F-30 | Tampering | Go/TS wire-contract drift silently breaking redaction assumptions on the render side | mitigate | `scripts/check_share_wire_contract.cjs` extracts the Go json tags and fails the build if the TS mirror lacks one. |
| T-37F-31 | Information Disclosure | a leak-capable field added to the TS type "for convenience" | mitigate | Grep gate: `path`/`arguments`/`sidecar`/`identity_id`/`tool_call_id` must not appear in `shareTypes.ts`. |
| T-37F-32 | Information Disclosure | owner's language fingerprint persisted into a public snapshot | mitigate | Language is never written to the snapshot; the public page uses the browser `Accept-Language` detector default. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | No new web dependency — `react`, `react-i18next`, and vitest are all present (RESEARCH §Environment Availability: "Framework install: none"). |
</threat_model>

<verification>
- `npx vitest run web/src/chat/artifacts web/src/i18n` (Windows Git Bash — WSL has no node)
- `npx tsc --noEmit -p web/tsconfig.json`
- `npx eslint web/src/chat/artifacts/renderers/assetSourceContext.ts`
- `node scripts/check_share_wire_contract.cjs`
- `bash scripts/check-file-size.sh`
- `wc -l web/src/i18n/resources.ts` ≤ 582
</verification>

<success_criteria>
The R-05 blocker is removed without touching a single renderer: `HtmlPreview` is byte-identical and will
work on the public page. Every existing artifact test passes unedited, proving the default preserves
today's behavior. The share i18n module carries en+it with recursive key parity in 4 LOC of `resources.ts`
delta, and the TS snapshot type is proven to mirror the Go json tags by a checked-in script.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-05-SUMMARY.md` when done.
Record the post-edit `wc -l web/src/i18n/resources.ts`.
</output>
