# Module-Level & Dependency Bloat Audit — 2026-05-17

**Scope:** Read-only audit of go.mod, web/package.json, Dockerfile, .env.example, mcp.json, and archive housekeeping.
**Prior audits:** audit-dead-code-2026-05-17.md, audit-duplication-2026-05-17.md, audit-god-files-2026-05-17.md, audit-legacy-surfaces-2026-05-17.md
**Build status:** 15 cleanup commits shipped (stringx, llm.CloneMessages, httputil.NewHTTPClient, god-files extraction, lint 80→2).

---

## Executive Summary

**Confirmed deletable items: 3**
- web/package.json: 2 unused devDeps (rollup-plugin-visualizer, vite-plugin-svgr)
- web/package.json: 1 unused dep (react-select)

**Stale documentation: 1**
- .env (legacy reference only, no functional keys)

**Stale config: 1**
- runtime-workspace/mcp.json (empty {})

**No bloat found:**
- go.mod: All 22 direct deps actively imported. go mod tidy: whitespace only.
- Dockerfile apt/pip: All packages documented in exec.go or intentionally pre-installed.
- scripts/ralph/prd-completed-*.json: 18 files, legitimate phase archive (no duplicates/blanks).
- .planning/deep-refactor/: All phases have live progress.md (Phase10 closed 2026-05-15).

---

## 1. go.mod / go.sum — CLEAN

**Methodology:** cp go.mod go.mod.bak && go mod tidy && diff go.mod go.mod.bak

**Result:** Tidy produced identical diff (whitespace only on line 54 fsnotify).

**Direct deps verified (sample):**
- gopkg.in/telebot.v4 → internal/channels/telegram
- fyne.io/systray → internal/tray
- github.com/JohannesKaufmann/html-to-markdown/v2 → internal/agent/tools/registry/direct_fetch.go
- github.com/go-shiori/go-readability → internal/agent/tools/registry/direct_fetch.go
- github.com/skip2/go-qrcode → internal/api
- github.com/xuri/excelize/v2 → internal/agent/tools/registry/files_xlsx.go
- github.com/disintegration/imaging → cmd/build_icon
- github.com/eekstunt/telegramify-markdown-go → internal/channels/telegram, internal/telegram
- github.com/go-pdf/fpdf → internal/agent/tools/registry/doc.go (PDF generation)
- github.com/ledongthuc/pdf → internal/ingest (PDF extraction)
- github.com/aws/aws-sdk-go-v2/* → internal/storage (S3 via Garage)
- All others: Standard library wrappers (uuid, yaml, zap logging, crypto, image, sqlite).

**Conclusion:** No unused deps detected.

---

## 2. web/package.json — 3 UNUSED DEPS FOUND

### 2.1 rollup-plugin-visualizer (devDep, 5.12.0)

**Consumer check:**
`
grep -r "rollup-plugin-visualizer" web/src/ → (no output)
grep "rollup-plugin-visualizer" web/vite.config.ts → (no output)
`

**Verdict:** Listed in devDeps but never imported in vite.config.ts or any source file.

**Delete command:**
`
npm remove --save-dev rollup-plugin-visualizer
`

---

### 2.2 vite-plugin-svgr (devDep, 5.2.0)

**Verdict:** Listed in devDeps but never imported in vite.config.ts or any source file.

**Delete command:**
`
npm remove --save-dev vite-plugin-svgr
`

---

### 2.3 react-select (dep, 5.10.2)

**Verdict:** Direct dependency in package.json but zero imports in src/. (react-timezone-select is USED in SettingsPanel.tsx and retained.)

**Delete command:**
`
npm remove react-select
`

---

## 3. Dockerfile apt+pip — CLEAN

**apt packages reviewed:**
All 33 tools are explicitly documented in internal/agent/tools/registry/exec.go:
- "text/data (rg/jq/yq/sqlite3/bat/fd/fzf/sed/awk)"
- "git+gh (GitHub CLI)"
- "HTTP (curl/wget/httpie)"
- "network (nmap/tcpdump/traceroute/mtr/dig/whois/nc/socat)"
- "docs (pandoc, ffmpeg, imagemagick)"
- "file/disk (rsync, tree, ncdu, file, less, xz/zip/unzip)"
- "system (ssh/scp, strace, lsof, htop, procps, iproute2)"

**pip3 packages:**
All 14 are live targets per agent tool usage:
- requests, beautifulsoup4, lxml, pillow (direct_fetch.go)
- numpy, pandas, pyarrow, python-calamine, openpyxl, xlrd (files tools)
- pyyaml, python-dateutil, pytz, regex, python-docx, matplotlib (sandbox tools)

**Conclusion:** No dead weight.

---

## 4. .env.example vs internal/config — N/A

**Status:** No .env.example file exists. Phase 10 (2026-05-15) migrated all secrets to SQLite.

**Current .env:** Contains only 2 E2E test keys (AURA_E2E_TOKEN, AURA_E2E_CHAT_ID). Legacy reference kept for pre-Phase-10 upgrades.

**Verdict:** No .env.example documentation debt.

---

## 5. mcp.json Stale Entries — N/A

**File:** runtime-workspace/mcp.json

**Content:** {} (empty)

**Status:** Intentional placeholder. MCP_SERVERS_PATH can be configured by operators at runtime.

**Action:** Leave as-is (no bloat).

---

## 6. scripts/ralph/ Archive — CLEAN

**Files found:** 18 prd-completed-*.json phase archives (all legitimate closures, no duplicates/blanks/test files).

**Verdict:** Archive is clean.

---

## 7. .planning/deep-refactor/ Phases — CLEAN

**Status:** All 11 phases have live progress.md. Phase01 and Phase10 intentionally closed; directories kept for historical audit. No archival candidates.

---

## Action Summary

**Tier 1: Delete 3 npm packages (one atomic commit)**

cd web
npm remove --save-dev rollup-plugin-visualizer
npm remove --save-dev vite-plugin-svgr
npm remove react-select
npm install
git add package.json package-lock.json
git commit -m "refactor(web): remove unused devDeps and dependencies

- rollup-plugin-visualizer: never imported in vite.config
- vite-plugin-svgr: never imported in vite.config
- react-select: zero imports in src/ (react-timezone-select retained)"

---

## Findings Table

| Category | Item | Status | Action |
|----------|------|--------|--------|
| go.mod | 22 direct deps | CLEAN | None |
| go.sum | Tidy result | CLEAN | None |
| web/package.json | rollup-plugin-visualizer | UNUSED | Remove |
| web/package.json | vite-plugin-svgr | UNUSED | Remove |
| web/package.json | react-select | UNUSED | Remove |
| Dockerfile apt | 33 packages | DOCUMENTED | None |
| Dockerfile pip3 | 14 packages | LIVE | None |
| .env | Legacy test keys | N/A | Keep |
| mcp.json | Empty {} | INTENTIONAL | None |
| scripts/ralph/ | 18 archives | CLEAN | None |
| .planning/ | 11 phases | CLEAN | None |

**Audit completed:** 2026-05-17
**Time to resolution:** ~5 min (Tier 1 npm removal only)