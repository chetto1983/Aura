---
phase: 37B-web-artifact-sidebar
plan: 02
type: execute
wave: 2
depends_on: ["37B-01"]
files_modified:
  - web/package.json
  - web/package-lock.json
  - web/src/chat/attachments/types.ts
autonomous: false
requirements: [WEBART-05, WEBART-07]
user_setup: []
must_haves:
  truths:
    - "docx-preview (Apache-2.0) + its jszip transitive dep are installed and resolvable"
    - "SheetJS xlsx 0.20.3 (Apache-2.0) is installed from the cdn.sheetjs.com tarball, NOT the frozen npm 0.18.5"
    - "Asset.source_kind TS union includes 'agent'"
  artifacts:
    - path: "web/package.json"
      provides: "docx-preview + xlsx (CDN) declared deps"
      contains: "docx-preview"
    - path: "web/src/chat/attachments/types.ts"
      provides: "widened Asset.source_kind union"
      contains: "'agent'"
  key_links:
    - from: "web/package.json"
      to: "cdn.sheetjs.com"
      via: "dependency URL for xlsx"
      pattern: "cdn\\.sheetjs\\.com"
  prohibitions:
    - "MUST NOT run `npm i xlsx` (installs frozen 0.18.5 with CVE-2023-30533 + CVE-2024-22363)"
    - "MUST NOT skip the legitimacy checkpoint before the installs — the packages originate from CONTEXT discussion, not an authoritative lookup"
    - "MUST NOT change the server list query ordering or any backend file — 37A substrate is unchanged"
---

<objective>
Establish the supply-chain foundation: install the two lazy-loaded preview libraries with a blocking legitimacy checkpoint, and widen the `Asset.source_kind` TS union to include `'agent'` (37A widened the backend via migration 0035 but not the frontend type — RESEARCH Pitfall 3). Without the widen, the `source_kind === 'agent'` filter (panel) and the D-15 split-fold type-error.

Purpose: Unblock the preview renderers (docx/xlsx chunks) and the agent-scoped derived view + rehydration fold.
Output: `docx-preview`+`jszip`+`xlsx`(CDN) in package.json; `Asset.source_kind` = `'web'|'telegram'|'cli'|'agent'`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md
@web/src/chat/attachments/types.ts
</context>

<artifacts_produced>
This plan produces:
- **web deps:** `docx-preview` (Apache-2.0) + transitive `jszip`; `xlsx` SheetJS CE 0.20.3 (Apache-2.0) from `https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`.
- **Widened type member:** `Asset.source_kind?: 'web' | 'telegram' | 'cli' | 'agent'` in `web/src/chat/attachments/types.ts`.
</artifacts_produced>

<tasks>

<task type="checkpoint:human-verify" gate="blocking-human">
  <name>Task 1: Package legitimacy gate — verify docx-preview, jszip, and the xlsx CDN tarball</name>
  <files>web/package.json, web/package-lock.json</files>
  <what-built>
    Nothing yet — this checkpoint gates the installs in Task 2. The two direct deps (`docx-preview`, `xlsx`) originate from CONTEXT.md discussion, not an authoritative session lookup, so they are treated as [ASSUMED] and verified before install. RESEARCH ran slopcheck 0.6.1 → 3 OK, but the `xlsx` CDN-tarball shape (a URL dependency in package.json) is an unusual supply-chain form a reviewer should consciously approve.
  </what-built>
  <how-to-verify>
    1. Open https://www.npmjs.com/package/docx-preview — confirm it exists, is Apache-2.0, is actively maintained (repo github.com/VolodymyrBaydalka/docxjs), and lists `jszip` as a dependency.
    2. Open https://www.npmjs.com/package/jszip — confirm it is the legitimate long-lived archive lib (github.com/Stuk/jszip).
    3. Open https://cdn.sheetjs.com/ (and https://docs.sheetjs.com/docs/getting-started/installation/nodejs) — confirm `xlsx-0.20.3` is the current SheetJS CE tarball and that the npm registry copy is the frozen 0.18.5 with CVE-2023-30533 + CVE-2024-22363. Confirm you accept a `cdn.sheetjs.com` URL dependency in package.json.
  </how-to-verify>
  <resume-signal>Type "approved" to proceed with the installs, or name a package to reject.</resume-signal>
</task>

<task type="auto">
  <name>Task 2: Install docx-preview + xlsx (CDN) and widen the source_kind union</name>
  <files>web/package.json, web/package-lock.json, web/src/chat/attachments/types.ts</files>
  <read_first>
    - web/package.json — current dependency block + npm engines; add the two deps in the existing style.
    - web/src/chat/attachments/types.ts:19 — the `Asset.source_kind` union to widen (PATTERNS "MODIFY — widen the union").
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Standard Stack" — exact install commands + the CDN-tarball rationale (Pitfall 2).
  </read_first>
  <action>
    In `web/`: run `npm i docx-preview` (pulls `jszip>=3.0.0`), then `npm i https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz` (do NOT `npm i xlsx`). If any transitive resolution drags `xlsx@0.18.5`, add a `package.json` `overrides` entry pinning `xlsx` to the CDN tarball. Then widen `Asset.source_kind` in `web/src/chat/attachments/types.ts` from `'web' | 'telegram' | 'cli'` to `'web' | 'telegram' | 'cli' | 'agent'`. Do not touch any other file. Run `npx tsc --noEmit` to confirm the union widen compiles cleanly across the existing consumers.
  </action>
  <acceptance_criteria>
    - `web/package.json` dependencies include `docx-preview` and an `xlsx` entry whose value contains `cdn.sheetjs.com` (URL dependency), NOT a bare semver `^0.18.x`.
    - `cd web && npm ls xlsx` resolves to 0.20.3 (or ≥0.20.2), never 0.18.5.
    - `cd web && npm ls jszip` resolves a version ≥3.0.0 (transitive of docx-preview).
    - `web/src/chat/attachments/types.ts` line for `source_kind` reads the four-member union including `'agent'`.
    - `cd web && npx tsc --noEmit` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "cdn.sheetjs.com" package.json && grep -q "docx-preview" package.json && grep -q "'agent'" src/chat/attachments/types.ts && npx tsc --noEmit && echo DEPS_AND_TYPE_OK</automated>
  </verify>
  <done>docx-preview+jszip+xlsx(0.20.3 from CDN) installed; `Asset.source_kind` union includes `'agent'`; `tsc --noEmit` clean.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| npm/CDN registry → build | Third-party package bytes enter the build; a malicious or vulnerable package is code-execution in the app + CI |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-SC | Tampering (supply chain) | npm installs `docx-preview`/`jszip` + `xlsx` CDN tarball | mitigate | Blocking-human legitimacy checkpoint (Task 1, never auto-approvable) verifies each package on its registry/CDN before install |
| T-37B-03 | Denial of Service / Tampering | `xlsx` version selection | mitigate | Install ≥0.20.2 from CDN (CVE-2023-30533 prototype-pollution HIGH + CVE-2024-22363 ReDoS both fixed); forbid npm 0.18.5; `overrides` pin if a transitive drags it |
| T-37B-04 | Tampering (type-safety hole) | `Asset.source_kind` union | mitigate | Widen the union so `=== 'agent'` filtering + the D-15 split-fold are type-checked, not `any`-cast |
</threat_model>

<verification>
- `npm ls xlsx` → 0.20.3 (never 0.18.5); `npm ls jszip` ≥3.0.0; `npm ls docx-preview` present.
- `npx tsc --noEmit` clean after the union widen.
</verification>

<success_criteria>
- The two lazy preview libs are installed with the CVE-safe xlsx from CDN, gated by an approved legitimacy checkpoint.
- `Asset.source_kind` includes `'agent'`; the project still type-checks.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-02-SUMMARY.md` when done.
</output>
</content>
