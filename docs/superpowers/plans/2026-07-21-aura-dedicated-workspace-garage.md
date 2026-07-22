# Aura Dedicated Workspace + Per-User Garage Object Store — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Aura a fixed local `/workspace` (pre-baked node/python toolchain) as the working root for `shell_exec`/`fs_*`/`send_file`, plus a durable per-user Garage bucket accessed via a forked, hardened S3 MCP — so document/data tasks stop hitting the ephemeral-fs + reinstall friction.

**Architecture:** Local POSIX `/workspace` volume for the live working tree + toolchain (S3 can't host a working tree); the per-identity Garage bucket `aura-<id>` (already built in `garageadmin`+`identity_store`, not yet activated) as the durable store; a forked `aura-mcp-s3` (from `txn2/mcp-s3`, Apache-2.0) mounted per-identity, hard-scoped to one bucket and binary/file-aware; `send_file` unchanged for delivery.

**Tech Stack:** Go 1.25+, pgx/sqlc (Postgres `aura.*`), Docker/compose, Garage (S3-compatible), the MCP stdio bridge (`internal/agent/mcptools`, `internal/mcp`), `internal/objectstore` (S3 + `garageadmin` + `identity_store`), `internal/agent/tools` (`shell_exec`, `fs_*`, `send_file`).

**Design source:** `docs/superpowers/specs/2026-07-21-aura-dedicated-workspace-garage-design.md` (proposed Amendment #88).

## Global Constraints

- **PRD-first:** the Amendment #88 commit (Task 0) lands in `prd.md` BEFORE any code (CLAUDE.md PRD-first).
- **File size:** no file > 600 LOC; split on touch (`<name>_<concern>.go`).
- **Post-edit gate (every Go edit):** `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/` (WSL, `CGO_ENABLED=1`).
- **Coverage floor:** owned-surface ≥85% across the `db_integration neo4j_integration` matrix (`bash scripts/coverage_docker.sh`, disposable DBs). Daemon/container-gated code needs daemon-free unit tests or the floor drops.
- **Containerize MCP:** the fork runs as a docker image (`docker/mcp-s3/`), never a host install (`[[containerize-never-install-mcp-on-host]]`), mirroring `docker/mcp-neo4j-cypher/`.
- **Env convention:** `AURA_<DOMAIN>_<UNIT>` (new: `AURA_WORKSPACE_DIR`).
- **No-skip-as-green:** integration/E2E tiers must actually run (they `t.Fatal` under `$CI` when env is unset).
- **DoD:** each phase E2E-validated on a real scenario, score >9.8 (`[[e2e-real-not-smoke]]`).
- **Commit discipline:** atomic; imperative subject + why body; `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. Do NOT push unless the phase-close push rule + operator approval apply.

---

### Task 0: Ratify PRD Amendment #88

**Files:**
- Modify: `prd.md` (append Amendment #88 to the amendments section; next free number verified = 88)

**Interfaces:**
- Produces: the ratified amendment that unblocks Tasks 1–5.

- [ ] **Step 1: Locate the amendments section** — `grep -n "Amendment #87" prd.md` to find where #87 lives; append #88 in the same format directly after it.
- [ ] **Step 2: Paste the Amendment #88 text** verbatim from the design doc §3 (`docs/superpowers/specs/2026-07-21-aura-dedicated-workspace-garage-design.md`).
- [ ] **Step 3: Verify** — `grep -c "Amendment #88" prd.md` returns ≥1.
- [ ] **Step 4: Commit**

```bash
git add prd.md
git commit -m "docs(prd): ratify Amendment #88 (dedicated workspace + per-user Garage store)"
```

---

### Task 1: Workspace config + volume + WorkspaceRoot wiring + prompt hint

**Files:**
- Modify: `internal/config/config.go` (add `WorkspaceDir` field + `AURA_WORKSPACE_DIR` parse + `defaultWorkspaceDir()`)
- Modify: `cmd/aura/serve_dispatch.go` (set `WorkspaceRoot: cfg.WorkspaceDir` on the `fs_*` tools — currently unset — and keep it on `shell_exec`/`send_file`)
- Modify: `cmd/aura/serve.go` (or the runner Deps wiring) — pass `Workspace: cfg.WorkspaceDir` into `runner.Deps`
- Modify: `internal/agent/prompt.go` (add a short `<workspace>` doctrine block near `<profile_context>`)
- Modify: `compose.yaml` (declare volume `aura-workspace`; mount `aura-workspace:/workspace` on the `aura` service; set `AURA_WORKSPACE_DIR: ${AURA_WORKSPACE_DIR:-/workspace}`; add a bootstrap that `mkdir -p /workspace/{scripts,artifacts,.toolchain,scratch}` and symlinks `/workspace/skills` → the export dir)
- Test: `internal/config/config_workspace_test.go`, `internal/agent/prompt_test.go`

**Interfaces:**
- Produces: `config.Config.WorkspaceDir string` (default `/workspace` in-container via env; `~/.aura/workspace` code default); consumed by `serve_dispatch.go` and `runner.Deps.Workspace`.

- [ ] **Step 1: Write the failing config test**

```go
// internal/config/config_workspace_test.go
func TestWorkspaceDir_DefaultAndOverride(t *testing.T) {
    t.Setenv("AURA_WORKSPACE_DIR", "")
    got := LoadDB().WorkspaceDir
    if got == "" || !filepath.IsAbs(got) {
        t.Fatalf("default WorkspaceDir must be a non-empty absolute path, got %q", got)
    }
    t.Setenv("AURA_WORKSPACE_DIR", "/workspace")
    if LoadDB().WorkspaceDir != "/workspace" {
        t.Fatalf("AURA_WORKSPACE_DIR override not honored")
    }
}
```

- [ ] **Step 2: Run it, verify FAIL** — `go test ./internal/config/ -run TestWorkspaceDir` → FAIL (`WorkspaceDir` undefined).
- [ ] **Step 3: Implement** — add `WorkspaceDir string` to `Config`, a `defaultWorkspaceDir()` (`AURA_WORKSPACE_DIR` else `filepath.Join(userHome, ".aura", "workspace")`, abs-normalized like `absRunDir`), and wire it in `Load`. Mirror the existing `RunDir`/`SkillsDir` patterns (`config.go:364-427`, `577-598`).
- [ ] **Step 4: Run it, verify PASS.**
- [ ] **Step 5: Write the prompt-doctrine test**

```go
// internal/agent/prompt_test.go — add to TestPrompt_* group
func TestPrompt_WorkspaceDoctrine(t *testing.T) {
    for _, needle := range []string{"/workspace", "artifacts/", "already installed"} {
        if !strings.Contains(SystemPrompt, needle) {
            t.Errorf("system prompt missing workspace doctrine %q", needle)
        }
    }
}
```

- [ ] **Step 6: Run it, verify FAIL; add the `<workspace>` block** — a few lines: "/workspace is your persistent working home (scripts/, artifacts/, .toolchain/, skills/ read-only). Put deliverables in artifacts/ and deliver them with send_file. The common toolchain (docx, python-docx, pandoc, …) is already installed — do not reinstall it." Keep the `<memory>` block byte-identical. This is a one-time `messages[0]` change; no golden hash pin exists (verify with `grep -rn PrefixHash internal/`).
- [ ] **Step 7: Wire `WorkspaceRoot`** in `serve_dispatch.go` on the `fs_*` registrations (they currently pass none → process cwd); keep `shell_exec`/`send_file` pointed at `cfg.WorkspaceDir`; pass `Workspace: cfg.WorkspaceDir` into `runner.Deps`.
- [ ] **Step 8: Full gate** — `go vet ./... && go build ./... && go test ./internal/config/ ./internal/agent/ ./cmd/aura/` then `go test -race` (WSL) on those.
- [ ] **Step 9: Commit** — `feat(workspace): fixed /workspace working root + AURA_WORKSPACE_DIR + prompt doctrine (Amendment #88)`.

---

### Task 2: Pre-bake the document/data toolchain into the aura image

**Files:**
- Modify: `Dockerfile` (the aura service image — add OS pkgs `pandoc file xxd`; `npm i -g docx`; `pip install --no-cache-dir python-docx openpyxl pandas`; export `ENV NODE_PATH=/usr/lib/node_modules`)
- Modify: `compose.yaml` (add `NODE_PATH: /usr/lib/node_modules` to the aura env if not inherited from the image)
- Test: `scripts/workspace_toolchain_smoke.sh` (new) — asserts the toolchain resolves inside the built image

**Interfaces:**
- Produces: an aura image where `require('docx')`, `import docx` (python-docx), and `pandoc`/`file`/`xxd` all succeed with no install.

- [ ] **Step 1: Write the smoke script (failing)**

```bash
#!/usr/bin/env bash
# scripts/workspace_toolchain_smoke.sh — run inside the aura container
set -euo pipefail
node -e "require('docx'); console.log('docx-js ok')"
python3 -c "import docx, openpyxl, pandas; print('py docx/openpyxl/pandas ok')"
for t in pandoc file xxd; do command -v "$t" >/dev/null || { echo "MISSING $t" >&2; exit 1; }; done
echo "toolchain smoke: ok"
```

- [ ] **Step 2: Run against the CURRENT image, verify FAIL** — `docker exec aura bash scripts/workspace_toolchain_smoke.sh` → FAIL (docx/pandoc/… missing; matches the inventory).
- [ ] **Step 3: Edit the Dockerfile** — add the apt line (`apt-get install -y --no-install-recommends pandoc file xxd`), `RUN npm install -g docx`, `RUN pip install --no-cache-dir python-docx openpyxl pandas`, `ENV NODE_PATH=/usr/lib/node_modules`. Keep layers cache-friendly (before the app copy where possible).
- [ ] **Step 4: Rebuild + redeploy** — `docker compose build aura && docker compose up -d aura`; wait `docker inspect -f '{{.State.Health.Status}}' aura` = healthy.
- [ ] **Step 5: Run the smoke, verify PASS** — `docker exec aura bash scripts/workspace_toolchain_smoke.sh` → `toolchain smoke: ok`.
- [ ] **Step 6: Commit** — `feat(workspace): pre-bake docx/python-docx/pandoc/file/xxd toolchain + NODE_PATH (Amendment #88)`.

> **Amendment #88.1 (2026-07-22, operator-approved) — Task 2b (follow-on to the committed Task 2 base):** the base toolchain landed (`8597f5bf`). The installed `docx`/`pptx`/`xlsx`/`pdf` skills (present in `/var/lib/aura/skills`) need more: OS `poppler-utils`/`qpdf`/`pdftk`/`tesseract-ocr` + **LibreOffice headless** `-writer`/`-calc`/`-impress` (shared `soffice` via `scripts/office/soffice.py`); npm-global `pptxgenjs`; pip `python-pptx`/`xlsxwriter`/`pypdf`/`pdfplumber`/`reportlab`/`pdf2image`/`pytesseract`/`Pillow`/`defusedxml`/`lxml`/`markitdown[pptx]`. Full GUI suite/`-base`/`-draw` stays out. Extend `scripts/workspace_toolchain_smoke.sh` to assert all of them (`soffice --version`, the pip imports, `require('pptxgenjs')`, `pdftoppm`/`pdftotext`/`qpdf`/`pdftk`/`tesseract` on PATH). Rebuild + `--force-recreate` + GREEN. Separate commit. PRD/ADR revised first (this file + `prd.md` §Amendment #88 + the design doc).

---

### Task 3: Activate the per-user Garage bucket provisioning

**Files:**
- Modify: `cmd/aura/serve_provisioning.go` (in the per-identity provisioning path — where `identityDirRoots` provisions mcp/skills/pyscripts/agents — add a `provisionObjectStore(ctx, identityID)` that, when the graph/objectstore is wired and the identity is not `local`, mints the bucket + key via `garageadmin` and persists via `identity_store`)
- Modify (if a seam is missing): `internal/objectstore/identity_store.go` (ensure an `EnsureForIdentity(ctx, identityID) (Credentials, error)` idempotent upsert exists; if not, add it — CreateBucket+CreateKey are idempotent-guarded, row is UPSERT)
- Test: `internal/objectstore/identity_store_provision_test.go` (daemon-free unit around the upsert/fail-closed logic with a fake garageadmin + fake pool), and extend `identity_store_integration_test.go` under `db_integration`

**Interfaces:**
- Consumes: `garageadmin.Client.CreateBucket`, `.CreateKey`, `garageadmin.BucketForIdentity` (existing); `identity_store` encryption (existing).
- Produces: `IdentityStore.EnsureForIdentity(ctx, identityID string) (Credentials, error)` — idempotent; `local`/empty → the shared bucket (no mint); other identity → its own `aura-<id>` bucket + scoped key, encrypted row; fail-closed on error.

- [ ] **Step 1: Write the failing unit test**

```go
func TestEnsureForIdentity_MintsOwnBucketAndIsIdempotent(t *testing.T) {
    fakeAdmin := &fakeGarageAdmin{} // records CreateBucket/CreateKey calls
    st := newTestIdentityStore(t, fakeAdmin)
    id := "00000000-0000-0000-0000-0000000000ab"
    got1, err := st.EnsureForIdentity(context.Background(), id)
    if err != nil { t.Fatal(err) }
    if got1.Bucket != "aura-"+id { t.Fatalf("bucket = %q, want aura-<id>", got1.Bucket) }
    got2, err := st.EnsureForIdentity(context.Background(), id) // idempotent
    if err != nil { t.Fatal(err) }
    if got2.AccessKey != got1.AccessKey { t.Fatal("second call must not re-mint a new key") }
    if fakeAdmin.createKeyCalls != 1 { t.Fatalf("CreateKey called %d times, want 1", fakeAdmin.createKeyCalls) }
}

func TestEnsureForIdentity_LocalUsesSharedBucket(t *testing.T) {
    st := newTestIdentityStore(t, &fakeGarageAdmin{})
    got, err := st.EnsureForIdentity(context.Background(), "local")
    if err != nil { t.Fatal(err) }
    if got.Bucket != "aura-assets" { t.Fatalf("local must map to shared bucket, got %q", got.Bucket) }
}
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/objectstore/ -run TestEnsureForIdentity` → FAIL.
- [ ] **Step 3: Implement `EnsureForIdentity`** — read the row; if present, decrypt + return; if absent and non-`local`, `CreateBucket(BucketForIdentity(id))` + `CreateKey(id)` (both idempotent-guarded), encrypt + UPSERT the row, return. `local`/empty → the shared `Credentials`. Fail-closed (any error → error, never a foreign/shared fallthrough for a non-local id).
- [ ] **Step 4: Run unit, verify PASS.**
- [ ] **Step 5: Wire into provisioning** — call `EnsureForIdentity` from `serve_provisioning.go` where identities are provisioned (guarded on objectstore being wired; best-effort log on miss so a Garage-less dev boot is not fatal, mirroring the mcp provisioning).
- [ ] **Step 6: Integration test (db_integration)** — a real identity provisions and re-resolves to its own bucket; `local` stays shared. Must actually run (`t.Fatal` under `$CI` if env unset).
- [ ] **Step 7: Gate** — `go vet/build/test` + `-race` (WSL) on `internal/objectstore` + `cmd/aura`.
- [ ] **Step 8: Commit** — `feat(objectstore): activate per-identity Garage bucket provisioning (Amendment #88)`.

---

### Task 4: Fork + harden the S3 MCP (`aura-mcp-s3`), mount per-identity

**Files:**
- Create: `docker/mcp-s3/` (fork of `txn2/mcp-s3`, vendored source + `Dockerfile` building `aura-mcp-s3:local`, mirroring `docker/mcp-neo4j-cypher/`)
- Create: `docker/mcp-s3/patches/` or direct source edits — a bucket-scope interceptor + a `put_file` tool + prefix confinement + a trimmed tool registration
- Modify: `cmd/aura/serve_provisioning.go` / the per-identity MCP config writer (`~/.aura/mcp/<id>/servers.json`) — add the `aura-mcp-s3` server entry with the identity's endpoint + bucket + scoped key (from `EnsureForIdentity`) + `S3_USE_PATH_STYLE=true`, `MCP_S3_EXT_READONLY=false`, and the new `MCP_S3_FIXED_BUCKET`/`MCP_S3_PREFIX` env the fork reads
- Test: Go table tests in the fork for the interceptor + `put_file` binary round-trip; an Aura-side test that the per-identity `servers.json` gets the entry

**Interfaces:**
- Consumes: `Credentials{Bucket, AccessKey, SecretKey}` from Task 3; the Garage endpoint from config.
- Produces: image `aura-mcp-s3:local` exposing `s3_list_objects`, `s3_get_object`, `s3_put_object`, `put_file`, `s3_delete_object`, `s3_presign_url` — all hard-scoped to `MCP_S3_FIXED_BUCKET` under prefix `MCP_S3_PREFIX`.

- [ ] **Step 1: Clone + vendor the upstream** — `git clone https://github.com/txn2/mcp-s3 docker/mcp-s3/upstream` (Apache-2.0), then read `pkg/tools/*.go` to locate the tool registration + the put/get handlers. Record the exact handler file paths before editing (READ BEFORE EDIT).
- [ ] **Step 2: Write the failing interceptor test** (in the fork) — a table test asserting that a call with `bucket="other"` is rewritten/rejected to the fixed bucket, and a call under a path outside `MCP_S3_PREFIX` is rejected.
- [ ] **Step 3: Implement the bucket-scope interceptor** — read `MCP_S3_FIXED_BUCKET`/`MCP_S3_PREFIX` at start; wrap each tool handler so the effective bucket is always the fixed one and the key is prefix-confined. Run test → PASS.
- [ ] **Step 4: Write the failing `put_file` test** — put a small binary (e.g. 8 bytes with a NUL) from a local temp path, then get it back, assert byte-identical (proves binary round-trip that string `content` can't guarantee).
- [ ] **Step 5: Implement `put_file`** — a tool taking `{path, key}`; reads the local file, `PutObjectStream(io.Reader)` with sniffed content-type. Run test → PASS.
- [ ] **Step 6: Trim + build the image** — drop `s3_list_buckets` and cross-bucket `copy` from registration; `docker build -t aura-mcp-s3:local docker/mcp-s3/`.
- [ ] **Step 7: Prove the image against Garage** (containerized, like the design's validation) — a scripted stdio handshake in a test harness OR a one-shot: `put_file` a temp docx-like binary → `s3_get_object` → byte-compare; assert a foreign-`bucket` call cannot escape the fixed bucket.
- [ ] **Step 8: Wire the per-identity mount** — extend the `servers.json` writer so a provisioned identity gets the `aura-mcp-s3` entry (docker-run command + the identity's env). Aura-side unit test asserts the entry shape (endpoint/bucket/key/path-style/fixed-bucket).
- [ ] **Step 9: Gate** — fork `go test`; Aura `go vet/build/test` + `-race` (WSL) on `cmd/aura` + the mcp config package.
- [ ] **Step 10: Commit** — `feat(mcp): fork + harden aura-mcp-s3 (bucket hard-scope + binary put_file), mount per-identity (Amendment #88)`.

---

### Task 5: Prompt/doctrine finalization + full-matrix + real E2E

**Files:**
- Modify: `docs/aura-quality-snapshot.md` (re-attest the rows whose CI-gate globs match this phase)
- Test/verify: the docx scenario end-to-end on the live stack

**Interfaces:**
- Consumes: everything from Tasks 1–4.

- [ ] **Step 1: Full owned-surface matrix** — `bash scripts/coverage_docker.sh` (disposable `aura_cov` + disposable neo4j). Must print `ok: owned coverage ≥85%`. Add daemon-free unit tests for any package that dropped below floor.
- [ ] **Step 2: Real E2E (score >9.8)** — rebuild+redeploy aura; drive an authenticated operator turn: *"Produci un documento Word con un breve report e mandamelo."* Assert: (a) the agent does NOT reinstall docx/pandoc (toolchain pre-baked), (b) the file is written under `/workspace/artifacts/`, (c) it persists after `docker compose up -d --force-recreate aura`, (d) it is durable in the operator's per-user bucket (verify via `aura-mcp-s3` `s3_list_objects` or an objectstore read), (e) delivered via `send_file`. Use the login+run driver from the Amendment #87 E2E recipe.
- [ ] **Step 3: Quality-snapshot re-attest** — run the gate locally: `AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" bash scripts/quality_snapshot_gate.sh` must print `ok:`; update any flagged row.
- [ ] **Step 4: Commit** — `feat(workspace): finalize doctrine + quality re-attest; docx E2E zero-friction (Amendment #88)`.
- [ ] **Step 5: Phase-close** — `go build ./...` + `go vet ./...` green; confirm with the operator before any `git push`; verify CI green after.

---

## Self-Review

**Spec coverage:** §2.1 local workspace → Task 1; §2.2 toolchain → Task 2; §2.3 per-user bucket → Task 3; §2.4 forked MCP → Task 4; §2.5 delivery + doctrine + §4 plan + risks → Tasks 1/5. All spec sections map to a task.

**Placeholder scan:** no "TBD/handle edge cases/similar to Task N". Task 4 Steps 1–3 explicitly READ upstream before editing (the exact upstream handler paths are discovered at clone time — this is honest, not a placeholder, because the upstream source is not vendored yet).

**Type consistency:** `EnsureForIdentity(ctx, identityID string) (Credentials, error)` (Task 3) is consumed by Task 4's mount wiring with the same `Credentials{Bucket, AccessKey, SecretKey}` shape from `identity_store.go`. `AURA_WORKSPACE_DIR`/`Config.WorkspaceDir` (Task 1) consumed by Task 2's compose env and Task 4's mount. `MCP_S3_FIXED_BUCKET`/`MCP_S3_PREFIX` are consistent between Task 4's fork and its mount env.
