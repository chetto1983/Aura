# HANDOFF — Amendment #88 (dedicated workspace + per-user Garage), resume at Task 2/5

**Written:** 2026-07-21 (end of the Task-1 session; started fresh next session).

## Read first, in order
1. `docs/superpowers/specs/2026-07-21-aura-dedicated-workspace-garage-design.md` — the ADR (proposed→ratified Amendment #88). Truth-source for the design.
2. `docs/superpowers/plans/2026-07-21-aura-dedicated-workspace-garage.md` — the 6-task implementation plan (Task 0..5). **This is what you execute.**
3. `.superpowers/sdd/progress.md` — the SDD ledger (git-ignored). Tasks marked complete there are DONE — do NOT re-dispatch; resume at the first unmarked task.
4. `prd.md` §Cross-cutting "Dedicated persistent workspace + per-user object store (Amendment #88)" — the ratified amendment.

Memories: `[[aura-phase-37a-web-artifact-delivery]]`, `[[shell-exec-runs-in-aura-container-nonstrict]]`, `[[containerize-never-install-mcp-on-host]]`, `[[prefer-industrial-libraries-over-bespoke]]`, `[[e2e-real-not-smoke]]`, `[[coverage-gate-nukes-neo4j]]`.

## Locked scope — "no more, no less"
Give Aura a fixed local `/workspace` (pre-baked node/python toolchain) as the working root for `shell_exec`/`fs_*`/`send_file`; a durable **per-user Garage bucket `aura-<id>`** (already-built infra, activate it) as the durable store; accessed via a **forked, hardened S3 MCP** (`aura-mcp-s3` from `txn2/mcp-s3`, Apache-2.0). Local FS for the working tree (S3 can't host it), object store for durability. Executed subagent-driven (superpowers:subagent-driven-development).

## DONE — on `master` (commit `a3746c84`), reconciled with origin/master
`master` = origin/master (`6a6f2f1e`, 130 commits) **+** the 3 workspace commits, **0 behind origin**. All green: `go build ./...`, `go vet ./...`, `-race` (WSL) on config/agent/runner/cmd-aura, pre-commit lint/dup/vet.

| Commit | Task |
|---|---|
| `43547e93` | **Task 0** — PRD Amendment #88 ratified (prd.md §Cross-cutting) |
| `f8cd1061` | **Task 1** — workspace config + wiring + prompt + compose |
| `a3746c84` | Merge origin/master (130) into the branch; 2 conflicts resolved; FF master |

**Task 1 detail (done + reviewed OK):**
- `config.WorkspaceDir` (env `AURA_WORKSPACE_DIR`, default `~/.aura/workspace` code / `/workspace` in-container). **Honored VERBATIM** (no `filepath.Abs` — that rewrote `/workspace`→`C:\workspace` on a Windows dev host and reddened the test; container path is already absolute). Helper in `internal/config/config_paths.go` (split for the 600-LOC cap — config.go is 583).
- `WorkspaceRoot: cfg.WorkspaceDir` on `shell_exec`, `fs_read/write/edit/grep/glob` (these had NO WorkspaceRoot before → process cwd), and `send_file` — wired in **`cmd/aura/main.go:191 buildBaseRegistryWithHandles`**, the SHARED builder every boot path (incl. `serve`) uses. **NOTE: the plan says `serve_dispatch.go`; that's STALE — origin refactored tool registration into `main.go`. Follow main.go.**
- `runner.Deps.Workspace = cfg.WorkspaceDir` (the per-turn "Working directory" hint) in `cmd/aura/chat_boot.go`.
- `<workspace>` static doctrine block in `internal/agent/prompt.go` (byte-stable → respects the KV-cache invariant #16/#29; `<memory>` left byte-identical). Needle test in `prompt_test.go`.
- `compose.yaml`: `aura-workspace` named volume mounted at `/workspace` + `AURA_WORKSPACE_DIR` env + a bootstrap `mkdir -p /workspace/{scripts,artifacts,.toolchain,scratch}` + `/workspace/skills` symlink to the export dir. **NOT yet rebuilt/redeployed** — that happens in Task 2.

## NEXT — Task 2/5 (execute in order, subagent-driven)
- **Task 2** — pre-bake the toolchain in the aura **Dockerfile**: apt `pandoc file xxd`; `npm i -g docx` + `ENV NODE_PATH=/usr/lib/node_modules`; `pip install python-docx openpyxl pandas`. Smoke script `scripts/workspace_toolchain_smoke.sh` (docx `require`, python-docx import, pandoc/file/xxd present). Rebuild+redeploy + run the smoke green. (Inventory confirmed the current image lacks pandoc/file/xxd/docx/python-docx.)
- **Task 3** — activate the per-user Garage bucket. Infra EXISTS but is NOT activated: `internal/objectstore/garageadmin` (`BucketForIdentity`→`aura-<id>`, `CreateBucket`, `CreateKey`) + `identity_store` (crypto-isolated, fail-closed). `aura.identity_object_store` is EMPTY; only the shared `aura-assets` bucket exists. Add `IdentityStore.EnsureForIdentity(ctx, id) (Credentials, error)` (idempotent; `local`→shared; other→own bucket+scoped key, encrypted row; fail-closed) + call it from `cmd/aura/serve_provisioning.go` per-identity provisioning + daemon-free unit + db_integration test.
- **Task 4** — fork `txn2/mcp-s3` → `docker/mcp-s3/` (containerized `aura-mcp-s3:local`, like `docker/mcp-neo4j-cypher/`). READ upstream `pkg/tools/*.go` FIRST. Mods: hard-scope to ONE bucket (`MCP_S3_FIXED_BUCKET`, override the per-call `bucket` arg), binary `put_file` (local path→upload, docx bytes round-trip), prefix confinement (`MCP_S3_PREFIX=workspace/`), trim `list_buckets`/`copy`. Mount per-identity via `~/.aura/mcp/<id>/servers.json` with the identity's endpoint+bucket+scoped key from Task 3.
- **Task 5** — full `db_integration neo4j_integration` matrix ≥85% (`bash scripts/coverage_docker.sh`, disposable DBs); real docx E2E (produce a Word report → written under `/workspace/artifacts/`, **zero reinstall**, persists across `docker compose up -d --force-recreate aura`, durable in the operator's per-user bucket, delivered via `send_file`), score >9.8; quality-snapshot re-attest.

## Validated facts (don't re-derive)
- **mcp-s3 ↔ Garage PROVEN**: `garage-s3` mounted in Claude Code (docker stdio, `txn2/mcp-s3`) → `claude mcp list` = ✔ Connected; `list/put/get/presign/delete` round-trip against `garage:3900` (path-style) OK. This is the fork base for Task 4.
- **Per-user bucket = built, not activated** (see Task 3).
- **Garage/assets mature**: `send_file` already ingests deliverables into the identity's bucket (Assets seam, wired in serve); `internal/objectstore/s3.go` full S3 (presign/get/put/list/delete); compose wires `AURA_OBJECTSTORE_BACKEND=garage`, bucket `aura-assets`, `garage-bootstrap`.

## Gotchas / discipline
- **`master` auto-syncs and moves FAST** (this session it went 130 commits behind mid-work). Work on a branch (`feat/aura-workspace-garage` exists at `a3746c84` = master); `git fetch` + reconcile with origin/master BEFORE merging; resolve conflicts by COMBINING both sides.
- **Subagents hit session limits** — the Task-1 implementer was cut off mid-report (recovered from the clean tree + ledger). Keep the ledger current; resume from it, never re-dispatch a completed task.
- **⚠️ Security follow-up:** the Garage secret key was written in plaintext into `C:\Users\chett\.claude.json` by the `garage-s3` test mount. Scrub it (`claude mcp remove garage-s3`) or leave it for Task-4 testing — decide, don't forget.
- **E2E recipe (reuse from Amendment #87):** operator UUID `b130c94d-a213-463a-a797-ec124104363a`; creds `AURA_E2E_AUTHULA_EMAIL`/`_PASSWORD` in `.env` (email has trailing spaces — trim); cockpit `http://127.0.0.1:9080`; Authula double-submit login (no Origin header, `__Host-` cookie sent explicitly, LF line endings); `POST /api/conversations` (key is `"ID"` uppercase) → `POST /agent/run` `{threadId, messages:[{id,role:"user",content}]}` SSE. Stack is up.
- Per-commit: gofmt + build + vet + test (+ `-race` WSL). Direct `git commit`; do NOT push (auto-sync + operator confirm).
