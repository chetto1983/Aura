---
phase: 37B-web-artifact-sidebar
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - prd.md
autonomous: true
requirements: [WEBART-05, WEBART-06, WEBART-07, WEBART-08]
must_haves:
  truths:
    - "prd.md documents the WEBART-05..08 requirement group as a named section"
    - "prd.md documents the Artefatti web sidebar surface (panel + preview modal + mobile drawer)"
    - "prd.md records the two new web deps with correct licenses (docx-preview Apache-2.0, SheetJS CE Apache-2.0 via CDN) and the sandboxed-iframe HTML policy"
    - "prd.md records the saved-conversation download-persistence behavior (D-14 durable query + D-15 split-fold)"
  artifacts:
    - path: "prd.md"
      provides: "37B PRD-amendment covering WEBART-05..08 + sidebar surface + preview deps + persistence"
      contains: "WEBART-05"
  key_links: []
  prohibitions:
    - "MUST NOT write any web/ source, package.json, or test file in this plan — this is a docs-only gate"
    - "MUST NOT record docx-preview as MIT (RESEARCH A4 corrects CONTEXT: it is Apache-2.0)"
    - "MUST NOT record `npm i xlsx` as the install path (frozen 0.18.5 + 2 CVEs) — the CDN tarball is the recorded source"
---

<objective>
Land the mandatory PRD-amendment commit that gates all 37B code (D-19, PRD-first absolute). The PRD ([prd.md](prd.md)) is the truth-source; the Artefatti sidebar surface, the WEBART-05..08 requirement group, the preview renderer set + the two new web deps + the sandboxed-iframe HTML policy, and the saved-conversation download-persistence behavior (D-14/D-15) are currently undocumented. This plan documents them BEFORE a line of implementation code is written.

Purpose: Satisfy the PRD-first principle (CLAUDE.md) and D-19; establish the architectural record every downstream 37B plan builds against.
Output: An amended prd.md committed as a PRD-amendment.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@.planning/phases/37B-web-artifact-sidebar/37B-CONTEXT.md
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
</context>

<artifacts_produced>
This plan produces:
- **prd.md amendment** — a new subsection under the web-cockpit / artifact-delivery area covering: (1) the WEBART-05..08 requirement group; (2) the "Artefatti" right-side sidebar surface (toggleable third `ResizablePanel`, mobile right `Drawer`, click-to-preview modal); (3) the preview renderer set (image/pdf/text/html/docx/xlsx; SVG + pptx → download-only) and the two new web deps — `docx-preview` **Apache-2.0** (+ `jszip` transitive) and **SheetJS CE `xlsx` Apache-2.0 installed from `https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`, NOT npm**; (4) the sandboxed-iframe HTML policy (`sandbox="allow-scripts"` null-origin, no `allow-same-origin`); (5) the saved-conversation download-persistence behavior (D-14 `threadId`-keyed durable query + D-15 `source_kind` split-fold).

No code symbols are produced by this plan.
</artifacts_produced>

<tasks>

<task type="auto">
  <name>Task 1: Amend prd.md with the 37B Artefatti sidebar + WEBART-05..08 + preview deps + persistence</name>
  <read_first>
    - prd.md — locate the web-cockpit / artifact-delivery section (the WEBART-01..04 delivery-lane content from 37A) to append the 37B amendment adjacently; match existing PRD heading/format conventions.
    - .planning/REQUIREMENTS.md:69-72 — the locked WEBART-05..08 acceptance text to transcribe faithfully.
    - .planning/phases/37B-web-artifact-sidebar/37B-CONTEXT.md D-01..D-19 — the decision record the amendment must reflect (esp. D-07 renderer set, D-14/D-15 persistence, D-19 scope).
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md — corrections to carry: docx-preview is **Apache-2.0** (A4), xlsx from **CDN ≥0.20.2** (Pitfall 2), no layout-key bump (Pattern 1).
  </read_first>
  <action>
    Add a "Web Artifact Sidebar (Artefatti) — 37B" subsection to prd.md adjacent to the existing WEBART delivery-lane content. Document: (1) the four requirements WEBART-05, WEBART-06, WEBART-07, WEBART-08 with their acceptance text; (2) the sidebar surface — a toggleable third right-side `ResizablePanel` in the `AppShell` chat shell that collapses to a `Drawer side="right"` below `lg`, a derived `useQuery(['assets', threadId])` view over `GET /api/assets?thread_id=` (no new source of truth, ownership via `GetForIdentity`), per-row + "Scarica tutto" downloads over `GET /api/assets/{id}/download`, and a click-to-preview modal; (3) the preview renderer set and the new deps — `docx-preview` **Apache-2.0** (transitive `jszip`) and **SheetJS Community Edition `xlsx` Apache-2.0 installed from the CDN tarball `https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`** (record the reason: the npm copy is frozen at 0.18.5 with CVE-2023-30533 + CVE-2024-22363), both lazy-loaded; SVG and pptx are download-only; (4) the HTML sandbox policy `sandbox="allow-scripts"` with NO `allow-same-origin` (null origin), and that untrusted bytes render only in blob-URL/iframe/DOM elements, never as a top-level document from our origin (works WITH 37A's `attachment`/`octet-stream` guard, not against it); (5) the saved-conversation download-persistence behavior — D-14 (the `threadId`-keyed query makes downloads durable on saved-conversation open with no reload) and D-15 (split `attachAssetsToUserMessages` by `source_kind`; fold `source_kind='agent'` assets onto assistant turns). Reference the env-var / dependency catalog conventions already in the PRD if a deps table exists there.
  </action>
  <acceptance_criteria>
    - `grep -c "WEBART-05" prd.md` ≥ 1 AND `grep -c "WEBART-08" prd.md` ≥ 1.
    - prd.md contains the literal string `cdn.sheetjs.com` and the string `docx-preview`.
    - prd.md contains `Apache-2.0` in the same subsection as `docx-preview` (license correction recorded).
    - prd.md contains the string `allow-scripts` and does NOT introduce `allow-same-origin` as an approved combination for the HTML preview.
    - prd.md contains a reference to the saved-conversation persistence behavior (search: `source_kind` OR `Scarica tutto` OR `Artefatti`).
    - No file under `web/` and no `package.json` is modified by this plan (`git diff --name-only` shows only `prd.md`).
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura && grep -q "WEBART-05" prd.md && grep -q "WEBART-08" prd.md && grep -q "cdn.sheetjs.com" prd.md && grep -q "docx-preview" prd.md && grep -q "allow-scripts" prd.md && echo PRD_AMENDMENT_OK</automated>
  </verify>
  <done>prd.md documents the WEBART-05..08 group, the Artefatti sidebar surface, the two Apache-2.0 preview deps (xlsx from CDN), the null-origin HTML sandbox policy, and the D-14/D-15 persistence behavior; only prd.md changed.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| author → PRD record | An inaccurate PRD (wrong license, wrong install source) misdirects every downstream implementation plan |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-01 | Tampering (documentation drift) | prd.md dep record | mitigate | Record `xlsx` CDN tarball ≥0.20.2 explicitly (not npm 0.18.5) so the supply-chain shape is auditable; record docx-preview as Apache-2.0 per RESEARCH A4 |
| T-37B-02 | Information Disclosure (policy record) | prd.md HTML sandbox policy | mitigate | Document the null-origin `allow-scripts` (no `allow-same-origin`) policy so the security posture is the recorded contract, not an implementation accident |
</threat_model>

<verification>
- `grep -q "WEBART-05" prd.md` and `grep -q "WEBART-08" prd.md` succeed.
- The PRD-amendment is a standalone commit landing BEFORE any 37B code commit (D-19).
</verification>

<success_criteria>
- prd.md records the WEBART-05..08 group, the Artefatti sidebar surface, the preview renderer set + two Apache-2.0 web deps (xlsx via CDN), the null-origin HTML sandbox policy, and the D-14/D-15 persistence behavior.
- Docs-only: no web source, package.json, or test file touched.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-01-SUMMARY.md` when done.
</output>
