---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 16
type: execute
wave: 3
depends_on: ["37F-05"]
files_modified:
  - web/src/routes/SharePage.tsx
  - web/src/routes/SharePage.test.tsx
  - web/src/main.tsx
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "/s/{token} renders a read-only conversation without a session"
    - "The page shows no owner name, no avatar, and no identity — nothing that identifies who shared it"
    - "The page has no composer, no regenerate, no continue, and no clone affordance"
    - "An HTML artifact renders in a null-origin iframe: sandbox=allow-scripts WITHOUT allow-same-origin, fed via srcDoc"
    - "An SVG artifact is download-only, never inline-rendered"
    - "Turn text containing an XSS payload renders escaped — no element is created from it"
    - "Unknown, expired, and revoked tokens all render the SAME body — no oracle"
    - "The page reuses the 37B renderers unedited via the asset-source provider"
  artifacts:
    - path: "web/src/routes/SharePage.tsx"
      provides: "the public read-only snapshot page at /s/:token"
      min_lines: 80
    - path: "web/src/main.tsx"
      provides: "the lazy /s/:token route"
      contains: "SharePage"
  key_links:
    - from: "web/src/routes/SharePage.tsx"
      to: "web/src/chat/artifacts/renderers/assetSourceContext.ts"
      via: "a provider returning token-scoped URLs with credentials omitted"
      pattern: "AssetSourceContext"
  prohibitions:
    - "MUST NOT render the owner's name, avatar, or identity — open-webui leaks exactly this via getUserInfoById; L-08 forbids it"
    - "MUST NOT fork HtmlPreview or any 37B renderer for the public page — they must work unedited through the asset-source provider"
    - "MUST NOT grant allow-same-origin to the artifact iframe — that would let the sandboxed script drop its own sandbox"
    - "MUST NOT render an SVG inline — previewKind returns download for image/svg+xml and that gate applies on the public path too"
    - "MUST NOT redirect to / on a missing link — open-webui does goto('/'); Aura's locked behavior is a 404 body"
    - "MUST NOT distinguish unknown vs expired vs revoked in the rendered body — that is an oracle"
    - "MUST NOT send credentials on the public asset path"
    - "MUST NOT render a composer, regenerate, continue, or clone affordance — the page is read-only; clone is the deferred remix idea"
    - "MUST NOT treat the router as an auth boundary — the server's 404 from the data fetch is the gate"
    - "MUST NOT persist or read the owner's language — fall back to the browser Accept-Language"
---

<objective>
Build the public read-only page at `/s/{token}` — the phase's only unauthenticated HTML surface.

The key architectural move is one open-webui got right: **reuse the same message renderer with a
read-only flag**, rather than writing a second one. That is also exactly why Aura's public page must be
the SPA and not a Go template — a Go template would be a second renderer with its own escaping and its own
field access, forking redaction and XSS policy, which is precisely what D-07 exists to prevent. Plan
37F-05's `AssetSourceContext` is what makes it possible: `HtmlPreview` and the other renderers work here
**unedited**.

Where open-webui is weaker, do not follow it:
- It **leaks the owner's PII** — `getUserInfoById(chat.user_id)` renders the owner's name and avatar on
  the public page. Aura omits it (L-08).
- It **redirects home** on a missing link (`goto('/')`). Aura's locked behavior is a 404 body.
- Its 403-vs-401 confusion once **signed viewers out** of shared read-only chats. Aura's
  404-on-everything posture avoids that class entirely.

Purpose: a recipient sees the redacted snapshot and nothing else.
Output: `SharePage.tsx` + a 2-LOC route.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@web/src/chat/share/shareTypes.ts
@CLAUDE.md
</context>

## Artifacts this plan produces

`SharePage`, the `/s/:token` route, and the token-scoped `AssetSourceContext` provider value.

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: SharePage — the read-only snapshot renderer with a token-scoped asset provider</name>
  <read_first>
    - `web/src/chat/artifacts/renderers/assetSourceContext.ts` — the seam (plan 37F-05). The page wraps its tree in a provider whose `assetUrl` returns the token-scoped path and whose `credentials` is `omit`.
    - `web/src/chat/artifacts/renderers/HtmlPreview.tsx` — **the whole file. Reused VERBATIM — do not fork it.** Its comment is the D-03 policy statement and the reason a Go-template renderer is forbidden: `sandbox="allow-scripts"` **without** the same-origin token makes the frame's origin null, so scripts run but `document.cookie` is empty, `window.parent` access throws, and `fetch('/api/…')` carries no ambient session. Granting `allow-same-origin` would let the sandboxed script drop its own sandbox.
    - `web/src/chat/artifacts/artifactMeta.ts:52` — `previewKind(mime, filename)`: the SVG-to-download gate (T-37B-05). Reuse verbatim on the public path.
    - `web/src/routes/LoginPage.tsx` — the structural precedent for a route that renders **without a session**. It is a form, not a document renderer, so nothing else transfers — but its class is the same.
    - `web/src/main.tsx:13-19,30-46` — the lazy-route registration shape and the header comment stating *"The router provides client navigation + a client-side 404 only. It is NOT an auth boundary — the server's RequireAuth is the real gate; we never hide a route purely client-side."* That rule governs this page: the server's 404 from the data fetch is the gate.
    - `web/src/chat/share/shareTypes.ts` — the `Snapshot` type this page renders (plan 37F-05)
    - `web/src/i18n/resources.share.ts` — the public-page strings
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"UI/UX Research" §4 — the page design, the open-webui weaknesses, and the no-oracle rule; §6 — the `Accept-Language` fallback
  </read_first>
  <behavior>
    - Mount with a valid token ⇒ fetches the snapshot data and renders the title, the snapshot date, and the turns in order
    - Each turn renders its role and text; turns with tool names show the provenance
    - Artifacts render through the 37B renderers, resolving bytes via the token-scoped provider
    - An HTML artifact renders in an iframe with `sandbox="allow-scripts"` and **no** `allow-same-origin`, fed via `srcDoc`
    - An `image/svg+xml` artifact resolves to the download kind, never an inline render
    - A turn whose text contains an XSS payload renders as text — no element is created from it
    - A 404 from the data fetch ⇒ the not-found body; **identical** for unknown, expired, and revoked
    - Never redirects to `/` on a missing link
    - Renders no owner name, avatar, or identity
    - Renders no composer, regenerate, continue, or clone control
    - Loading shows a `role="status"`; a network error shows a `role="alert"`
  </behavior>
  <action>
    Create `web/src/routes/SharePage.tsx`.

    Read the token from the route param, fetch `GET /s/{token}/data` (**no credentials** — the recipient
    has no session, and sending cookies to an unauthenticated route is needless), and render the
    `Snapshot`.

    Wrap the artifact subtree in an `AssetSourceContext.Provider` whose value resolves
    `/s/{token}/asset/{assetId}` with `credentials: 'omit'`. **This is the whole payoff of plan 37F-05's
    seam**: `HtmlPreview` and the other renderers then work here with zero edits. If you find yourself
    editing a renderer, stop — the seam is wrong, not the renderer.

    Page content: title + snapshot date + a discreet "shared from Aura" mark. **No owner name, no avatar,
    no identity** — open-webui renders the owner's profile here and Aura must not (L-08). Read-only: no
    composer, no regenerate, no continue. **No clone affordance** — that is the deferred remix idea.

    **Missing/expired/revoked ⇒ one body, one status.** Render the same not-found content for all three.
    Distinguishing them is an oracle: "expired" confirms the token *was* valid. Do **not** `goto('/')` —
    that is open-webui's behavior and it is weaker than the locked 404.

    i18n: the recipient has no Aura language preference, so fall back to the browser `Accept-Language` via
    the i18next detector default. **Do not** read or persist the owner's language — it is
    fingerprinting-adjacent and a needless coupling.

    Aesthetics (CLAUDE.md): this page is a stranger's first impression of Aura. Use the existing **BLUE**
    cockpit tokens — approved, do not re-skin — and the established type treatment. One orchestrated
    reveal on load using the staggered `animate-in fade-in-0 slide-in-from-bottom-1` idiom already in
    `ArtifactsPanel.tsx:121-122`. Do not scatter micro-interactions.

    Write `SharePage.test.tsx` covering every `<behavior>` row, with the **security assertions** as the
    centrepiece (they are also VALIDATION.md's web rows):
    - An HTML artifact's iframe: assert the `sandbox` attribute string **exactly** (`allow-scripts`, and
      that it does **not** contain `allow-same-origin`), and that it is fed by `srcDoc`, not `src`.
    - `image/svg+xml` ⇒ `previewKind` returns download; assert no inline `<svg>`/`<img>` renders.
    - A turn containing an `<img src=x onerror=alert(1)>` payload ⇒ assert the DOM contains **no** `img`
      element (React escapes by default; this pins it).
    - Unknown / expired / revoked ⇒ assert the rendered body is identical across all three.
    - Assert no owner name/avatar appears given a snapshot that carries none.
  </action>
  <verify>
    <automated>npx vitest run web/src/routes && npx tsc --noEmit -p web/tsconfig.json && npx eslint web/src/routes/SharePage.tsx</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/routes` passes, covering every `<behavior>` row.
    - **The sandbox assertion is exact:** the test asserts the attribute equals `allow-scripts` and explicitly asserts `allow-same-origin` is ABSENT — not a substring match that would pass on `allow-scripts allow-same-origin`.
    - **The XSS-in-text test asserts the DOM has no `img` element**, not merely that the text is present.
    - **The SVG test asserts no inline render**, matching the T-37B-05 gate on the public path.
    - **The no-oracle test asserts byte/DOM-identical bodies** across unknown, expired, and revoked.
    - **No renderer was forked:** `git diff --name-only` lists no file under `web/src/chat/artifacts/renderers/`.
    - `grep -c "credentials: 'omit'\|credentials: \"omit\"" web/src/routes/SharePage.tsx` returns ≥1.
    - `grep -ciE "goto\(|navigate\('/'\)|redirect" web/src/routes/SharePage.tsx` returns `0` — no redirect-home on miss.
    - `grep -ciE "avatar|ownerName|user_id|getUserInfo" web/src/routes/SharePage.tsx` returns `0`.
    - `grep -ciE "composer|regenerate|clone" web/src/routes/SharePage.tsx` returns `0`.
    - No literal user-facing strings — every visible string comes from `t('share.…')`.
    - `npx eslint web/src/routes/SharePage.tsx` reports 0 errors; `npx tsc --noEmit` clean; the file is ≤600 LOC.
    - vitest coverage for `web/src/routes/SharePage.tsx` ≥ 85%.
  </acceptance_criteria>
  <done>`SharePage` renders the redacted snapshot read-only with no owner PII and no interaction affordances, reuses every 37B renderer unedited through a token-scoped provider, isolates HTML in a null-origin iframe, keeps SVG download-only, escapes XSS payloads, and returns one indistinguishable not-found body.</done>
</task>

<task type="auto">
  <name>Task 2: main.tsx — the lazy /s/:token route</name>
  <read_first>
    - `web/src/main.tsx:13-19` — the `lazy(() => import('./routes/X').then((mod) => ({ default: mod.X })))` named-export shape; `:30-46` — the `<Routes>` table, the `/c/:id` inline comment naming its decision, and the header comment: *"The router provides client navigation + a client-side 404 (NotFoundView) only. It is NOT an auth boundary — the server's RequireAuth is the real gate; we never hide a route purely client-side (T-24-21)."*
    - `cmd/aura/serve_webui_share.go` — `isPublicShareRoute` (plan 37F-12): the server side that admits `/s/` to the SPA shell. Confirm the path shape matches this route exactly.
    - `internal/agui/auth.go:167-170` — the note that the static bundle is already public for everyone, which is why serving the SPA at `/s/{token}` costs no extra asset gate
  </read_first>
  <action>
    Edit `web/src/main.tsx` with **2 LOC**: one `lazy(...)` import for `SharePage` following the existing
    named-export shape, and one `<Route path="/s/:token" element={<SharePage />} />` line in the table.

    Add an inline `{/* … */}` comment naming the decision, as `/c/:id` does: the public read-only share
    page renders **without a session**; the server admits `/s/` to the SPA shell via `PublicRoute`
    (`serve_webui_share.go`), and the server's 404 from `GET /s/{token}/data` — not the router — is the
    gate. Reinforce, do not contradict, the file header's rule that the router is not an auth boundary.

    That is the whole `main.tsx` change.
  </action>
  <verify>
    <automated>npx vitest run web/src && npx tsc --noEmit -p web/tsconfig.json && npx eslint web/src/main.tsx</automated>
  </verify>
  <acceptance_criteria>
    - `grep -c "SharePage" web/src/main.tsx` returns `2` (the lazy import + the route element).
    - `grep -q 'path="/s/:token"' web/src/main.tsx`.
    - The route path shape matches the server predicate: `isPublicShareRoute` admits GET `/s/` prefixes (plan 37F-12), and `/s/:token` falls inside it.
    - An inline comment names the decision and states the server-side 404 is the gate.
    - The existing routes are unchanged: `git diff web/src/main.tsx` shows only additions.
    - `npx vitest run web/src` passes; `npx tsc --noEmit` clean; `npx eslint web/src/main.tsx` reports 0 errors.
    - `wc -l web/src/main.tsx` ≤ 600.
  </acceptance_criteria>
  <done>`/s/:token` is registered as a lazy route in 2 LOC with an inline comment stating the server's 404 — not the router — is the gate.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| untrusted agent HTML → the Aura origin | The null-origin iframe. `sandbox="allow-scripts"` WITHOUT `allow-same-origin` makes the frame's origin null, so the content cannot read cookies, reach the parent DOM, or carry an ambient session to `/api/…`. Granting the token would let the script drop its own sandbox. |
| a stranger's browser → Aura | `/s/{token}` is the only page an unauthenticated visitor can reach. Everything it renders is recipient-visible by definition. |
| router → auth | The router is **not** a boundary. The server's 404 is. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-04 | Information Disclosure | XSS via an HTML artifact on the public page | mitigate | Reuse `HtmlPreview` verbatim: null-origin `<iframe srcDoc sandbox="allow-scripts">` with no `allow-same-origin` (37B D-07). Test asserts the attribute exactly and asserts `allow-same-origin` is absent. A sanitizer is explicitly not used — sanitizers are a bypass treadmill; a null origin is structural. |
| T-37F-74 | Information Disclosure | XSS via an SVG artifact | mitigate | `previewKind` returns download for `image/svg+xml` (T-37B-05), asserted on the public path too. |
| T-37F-75 | Information Disclosure | XSS via snapshot turn text | mitigate | React escapes by default; the test pins it by asserting the DOM contains no `img` element for an `<img src=x onerror=…>` payload. |
| T-37F-13 | Information Disclosure | owner PII on the public page | mitigate | The `Snapshot` carries no identity (L-08) and the page renders no name/avatar. open-webui's `getUserInfoById` leak is the anti-pattern. Grep-gated. |
| T-37F-50 | Information Disclosure | token oracle via a distinguishable rendered body | mitigate | One not-found body for unknown/expired/revoked, asserted identical across all three. No redirect-home (open-webui's `goto('/')`). |
| T-37F-28 | Information Disclosure | cookies sent to the unauthenticated asset path | mitigate | The provider supplies `credentials: 'omit'`; grep-gated. |
| T-37F-76 | Elevation of Privilege | a second renderer forking redaction/escaping policy | mitigate | The SPA reuses the 37B renderers unedited via the asset-source seam; a Go-template renderer is forbidden by D-07 and by this plan. Enforced by a `git diff` gate on the renderers directory. |
| T-37F-77 | Elevation of Privilege | a client-side route mistaken for an auth boundary | accept | Documented inline and in `main.tsx`'s existing header; the server's 404 is the gate (T-24-21). No client-side mitigation is appropriate. |
| T-37F-32 | Information Disclosure | owner language fingerprint on the public page | mitigate | `Accept-Language` fallback; the owner's language is never persisted into the snapshot or read here. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | No new web dependency. |
</threat_model>

<verification>
- `npx vitest run web/src/routes web/src` (Windows Git Bash — WSL has no node)
- `npx tsc --noEmit -p web/tsconfig.json`
- `npx eslint web/src/routes/SharePage.tsx web/src/main.tsx`
- `git diff --name-only web/src/chat/artifacts/renderers/` → **empty** (no renderer forked)
- vitest coverage for `web/src/routes/SharePage.tsx` ≥ 85%
- Manual (VALIDATION.md Manual-Only): `docker compose build aura && up -d`, mint a public link, open `/s/{token}` in a **private window** (no session), and inspect the iframe attributes in devtools. Confirm no console errors and correct sandboxing. Inspect the page visually — do not trust a PASS status.
</verification>

<success_criteria>
A stranger with a link sees the redacted conversation and nothing more: no owner identity, no composer,
no clone, no other identity's data, no host path. HTML artifacts are confined to a null origin, SVGs are
download-only, XSS payloads render as text, and unknown/expired/revoked are indistinguishable. Not one
37B renderer was forked to achieve it.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-16-SUMMARY.md` when done.
Confirm no renderer was edited and record the SharePage coverage.
</output>
