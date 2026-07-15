---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 19
type: execute
wave: 9
depends_on: ["37F-18"]
files_modified:
  - .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
autonomous: false
requirements: [WEBSHARE-01, WEBSHARE-02, WEBSHARE-03, WEBSHARE-04]

must_haves:
  truths:
    - "A human confirms the public page renders correctly to a stranger with no session"
    - "A human confirms the iframe is sandboxed allow-scripts WITHOUT allow-same-origin in a real browser"
    - "A human confirms the user's own upload does NOT appear on a public share"
    - "A human confirms public is never preselected in the share modal"
    - "The phase is committed atomically and pushed to master"
    - "Every CI job is green, including BOTH coverage gates, the dist freshness gate, and the quality-snapshot gate"
  artifacts:
    - path: ".planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md"
      provides: "the closed-out validation contract"
      contains: "nyquist_compliant: true"
  key_links:
    - from: "the pushed commit"
      to: "docs/adr/0039-conversation-sharing-vs-identity-isolation.md"
      via: "the commit body naming the bounded MUSR exception"
      pattern: "0039"
  prohibitions:
    - "MUST NOT push before plan 37F-18's local matrix is green"
    - "MUST NOT dismiss a red CI job as out-of-scope — CI was green before this phase, so every red job now is this phase's to fix, including frontend and sidecar jobs"
    - "MUST NOT use a --files-sweeping commit wrapper — on a dirty tree it engulfs unstaged parallel work; git add explicitly and verify with git show --stat"
    - "MUST NOT verify against a stale container — the dist is baked into the image, so docker compose build is required first"
    - "MUST NOT check only the Knowledge coverage job — the Skills job is a separate, stricter db_integration-ONLY gate; verify THAT number too"
---

<objective>
Verify by hand what no test runner can, then ship.

Two properties of this phase are visual/behavioral and a green suite does not establish them:
- **Does the public page actually render correctly to a stranger?** A unit test asserts the `sandbox`
  attribute string; only a real browser proves the browser applied it, that no console error fires, and
  that nothing identifying the owner leaks into the render.
- **Does the modal actually default to internal?** D-01's "public is never default" is the phase's
  central promise to the user, and it is a thing you look at.

Project rule: *"Inspect artifact visually, not just PASS status."*

Then push, and drive CI green — including the **Skills** job, which is a separate `db_integration`-ONLY
coverage gate that is stricter than the Knowledge one and is the number that actually gates this phase.

Purpose: ship it, verified by a human.
Output: an approved checkpoint, a green CI, a closed-out VALIDATION.md.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@CLAUDE.md
</context>

## Artifacts this plan produces

The completed `37F-VALIDATION.md` and the shipped phase.

<tasks>

<task type="checkpoint:human-verify" gate="blocking">
  <name>Task 1: Live verification of the public page and the share modal</name>
  <what-built>
    The full 37F share surface: conversation export (MD/JSON), internal + public share links with a
    capability gate, mandatory expiry, revoke, the redacted snapshot, the audited ledger, the "Condiviso"
    section, the Settings shared-links list, and the public read-only page at `/s/{token}`.

    Every automated gate is already green (plan 37F-18): the Go tag matrix at ≥85% per package, the web
    gates at ≥85% coverage and ≥70% mutation, the SC3 core surviving mutation with no leak-class survivor,
    a fresh bundle, and an honest quality snapshot.

    What remains is what a test runner cannot establish: whether the public page **actually renders
    correctly to a stranger with no session**, whether the browser **actually applied** the iframe sandbox,
    and whether the modal's defaults **look** right.
  </what-built>
  <how-to-verify>
    1. **Bring the stack up with a freshly-baked bundle:**
       `docker compose build aura && docker compose up -d`
       The dist is baked into the image — skip the rebuild and you will verify stale UI.

    2. **Share modal — the D-01 check.** Open a conversation that has **both** an agent-produced artifact
       **and** a file you uploaded yourself. Click the share arrow in the floating cluster (top-right of
       the chat, between the voice and artifacts toggles). Confirm:
       - The **internal** tier is preselected and **public is NOT**.
       - The public warning is **absent** until you select public, then appears — and mentions that
         revoking does not remove copies already cached by search engines.
       - The expiry chips appear only for public, with **7 days** preselected.
       - The snapshot-frozen note is visible **before** you mint.

    3. **Mint a public link.** Confirm the URL renders and that **Copy** is a separate button (not an
       automatic copy on mint). Copy it.

    4. **The public page — the SC3 + D-09 check.** Open the URL in a **private/incognito window** (no
       session). Confirm:
       - The conversation renders read-only: no composer, no regenerate, no clone.
       - **No owner name and no avatar** appear anywhere.
       - The **agent artifact** is present, and **your own uploaded file is NOT**. This is D-09 amended —
         a user's upload must never enter a share.
       - **No filesystem path** appears anywhere on the page.

    5. **The iframe — the D-03 check.** If the thread has an HTML artifact, open devtools and inspect the
       iframe element. Confirm `sandbox="allow-scripts"` and that it does **NOT** contain
       `allow-same-origin`. Confirm the console shows no errors.

    6. **Revoke.** Back in the authenticated window, revoke the link (a confirm dialog must appear).
       Reload the public URL in the private window. Confirm a **404 body** — not a redirect home, and no
       trace of the conversation title.

    7. **Audit.** In the admin audit UI, confirm `share` rows appear for create and revoke.
  </how-to-verify>
  <resume-signal>Type "approved" or describe what looked wrong (a screenshot of the public page plus the iframe attributes from devtools is ideal).</resume-signal>
</task>

<task type="auto">
  <name>Task 2: Commit, push, and drive CI green</name>
  <read_first>
    - `CLAUDE.md` §"GIT PUSH DISCIPLINE" — push at the end of a phase and check every CI job is green
    - `CLAUDE.md` §"Commit discipline" — one slice = one commit; atomic; imperative subject; body explaining *why*; Co-Authored-By trailer
    - `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` — the ADR the commit body should point at
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the Per-Task Verification Map and Sign-Off to close out
  </read_first>
  <action>
    Commit, push, and drive CI green.

    **Stage explicitly.** `git add` the intended paths, then verify with `git show --stat`. Do **not** use
    a `--files`-sweeping wrapper — on a dirty tree it engulfs unstaged parallel work.

    **Use direct `git commit`** — the GSD commit wrapper times out on the file-size hook (which scans the
    whole tree). On a large dirty tree even direct commit can exceed the default Bash timeout; background
    it if needed.

    Commit message: imperative subject, body explaining **why**. This phase opens a deliberate, bounded
    hole in MUSR identity isolation — say so and point at ADR 0039. Note the two PRD amendments (D-08
    reasoning dropped; D-13 hash-indexed equality). Include the Co-Authored-By trailer per project
    convention.

    Push to **master** (master-direct workflow — no feature branch or PR unless asked).

    Then **watch every CI job**. CI was green before this phase, so **any** red job now is this phase's to
    fix — including a frontend or sidecar job that looks unrelated. Do not dismiss one as out-of-scope.
    Candidates, all of which plan 37F-18 should have pre-empted:
    - the **Knowledge/coverage** job (the two-tag gate; ~20 min to fail),
    - the **Skills** job — a **separate, stricter `db_integration`-ONLY coverage gate**. There are TWO
      coverage gates on this project; verify the Skills number too, not just the Knowledge one. It is the
      one that most often surprises.
    - the **dist freshness** gate,
    - the **quality-snapshot freshness** gate,
    - `vulncheck`,
    - the web jobs.

    Fix any red job and re-push until every job is green.

    Finally, close out `37F-VALIDATION.md`: confirm the Per-Task Verification Map is fully populated with
    real task IDs and green statuses (plan 37F-13 populated it), tick every Sign-Off box, and set
    `nyquist_compliant: true` and `status: complete`.
  </action>
  <verify>
    <automated>gh run list --limit 3</automated>
  </verify>
  <acceptance_criteria>
    - The commit is atomic, has an imperative subject, a body explaining **why** (naming the bounded MUSR exception, ADR 0039, and the two PRD amendments), and the Co-Authored-By trailer.
    - `git show --stat` confirms only the intended paths were staged — no parallel work swept in.
    - The push succeeded to master.
    - **Every CI job is green**, verified with `gh run list` / `gh run view`. Specifically green: the Knowledge coverage job, the **Skills db_integration-only** coverage job, the dist freshness gate, the quality-snapshot gate, `vulncheck`, and the web jobs.
    - No red job was dismissed as out-of-scope.
    - `37F-VALIDATION.md` has `status: complete`, `nyquist_compliant: true`, a fully populated Per-Task Verification Map with no `{TBD-planner}` placeholder, and every Sign-Off box ticked — including "No 37F test carries a tag outside `db_integration neo4j_integration`" and "SC4 lives in `internal/agui`, NOT `cmd/aura`".
    - The final CI run URL is recorded in the SUMMARY.
  </acceptance_criteria>
  <done>The phase is committed atomically with a why-bearing body, pushed to master, every CI job (both coverage gates included) is green, and `37F-VALIDATION.md` is closed out as nyquist-compliant.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| a test-runner PASS → the feature actually working | The public page's sandboxing and the modal's defaults are properties a green suite does not establish. The human checkpoint is the boundary. |
| a stranger's browser → Aura | Verified once, live, against the real container — the only place the browser's actual sandbox enforcement can be observed. |
| a green local tree → a green CI | Two coverage gates exist; the stricter Skills one is the usual surprise. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-04 | Information Disclosure | the iframe sandbox not actually applied by the browser | mitigate | The checkpoint inspects the live iframe attributes in devtools. A unit test asserts the string; only a real render proves enforcement. |
| T-37F-09 | Information Disclosure | a user's own upload reaching a live public page | mitigate | The checkpoint explicitly seeds a thread with BOTH an agent artifact and a user upload and requires confirming the upload is absent — the single highest-consequence check in the phase. |
| T-37F-69 | Elevation of Privilege | public preselected in the real UI despite a passing test | mitigate | The checkpoint looks at the modal on open and confirms internal is preselected and the warning is absent until public is chosen. |
| T-37F-13 | Information Disclosure | owner PII rendering on the live public page | mitigate | The checkpoint confirms no owner name or avatar appears in a real private-window render. |
| T-37F-01 | Information Disclosure | a stale render after revoke | mitigate | The checkpoint revokes and reloads the public URL, confirming a 404 body with no title. |
| T-37F-83 | Repudiation | dismissing a red CI job as unrelated | mitigate | CI was green before this phase; the plan states every red job is this phase's to fix and names the likely candidates, including the stricter Skills db_integration-only gate. |
| T-37F-85 | Tampering | a commit sweeping in unstaged parallel work | mitigate | Explicit `git add` + `git show --stat` verification; no `--files`-sweeping wrapper. |
| T-37F-86 | Repudiation | verifying against a stale container and approving stale UI | mitigate | `docker compose build aura` is step 1 of the checkpoint — the dist is baked into the image. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | No dependency added in this plan. |
</threat_model>

<verification>
- Blocking human checkpoint: public page renders to a stranger; iframe `sandbox="allow-scripts"` with no `allow-same-origin`; user upload absent; no owner PII; no host path; modal defaults to internal; revoke → 404 body
- `gh run list` / `gh run view` → every job green, both coverage gates included
- `37F-VALIDATION.md` → `status: complete`, `nyquist_compliant: true`, map populated, sign-off ticked
</verification>

<success_criteria>
37F ships. All four WEBSHARE requirements are implemented, proven by machine, and confirmed by a human
looking at the real thing: a stranger with a link sees the redacted conversation and nothing else — no
owner identity, no host path, no other identity's data, and not the user's own uploaded file — inside a
correctly sandboxed frame. Every CI job is green.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-19-SUMMARY.md` when done.
Record the checkpoint outcome and the final CI run URL.
</output>
