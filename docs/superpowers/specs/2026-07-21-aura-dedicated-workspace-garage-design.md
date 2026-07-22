# Aura — Dedicated persistent workspace + per-user Garage object store (design + ADR)

**Design date:** 2026-07-21
**Related:** `docs/audit/agentmd-supersession-design-2026-07-21.md` (Amendment #87) · memories `[[aura-phase-37a-web-artifact-delivery]]`, `[[shell-exec-runs-in-aura-container-nonstrict]]`, `[[sandbox-enablement-plan-ubuntu]]`, `[[containerize-never-install-mcp-on-host]]`, `[[prefer-industrial-libraries-over-bespoke]]`.

## 0. Locked scope (operator, 2026-07-21) — decided via brainstorming Q&A

Give Aura a **dedicated persistent workspace** where it stores scripts, artifacts, and a node/python toolchain, plus a **durable per-user object store**, so document/data tasks stop hitting the ephemeral-filesystem + reinstall friction seen in the docx conversation `019f83d0`.

Decisions taken:
- **Location:** a persistent local `/workspace` on the *current* non-strict aura container (usable now on Docker Desktop), on the **same fixed path** as the future sandbox box `/workspace` (forward-compatible with the mini-PC sandbox).
- **Toolchain:** common **pre-bake** sized to the installed `docx`/`pptx`/`xlsx`/`pdf` skills (npm `docx`+`pptxgenjs` with `NODE_PATH`; pip `python-docx`/`python-pptx`/`openpyxl`/`xlsxwriter`/`pandas`/`pypdf`/`pdfplumber`/`reportlab`/`pdf2image`/`pytesseract`/`Pillow`/`defusedxml`/`lxml`/`markitdown[pptx]`; OS `pandoc`/`file`/`xxd`/`poppler-utils`/`qpdf`/`pdftk`/`tesseract-ocr`) + a persistent `/workspace/.toolchain` for extra installs. **[Revised 2026-07-22, Amendment #88.1 — operator-approved:** LibreOffice **headless modules** (`-writer`/`-calc`/`-impress`) ARE pre-baked — the docx/pptx/xlsx skills share `scripts/office/soffice.py` and need `soffice` for render/formula-recalc/Office↔PDF. Full GUI suite/`-base`/`-draw` stays out.**]
- **Layout:** `/workspace = {scripts/, artifacts/, .toolchain/, scratch}`; skills stay in `~/.aura` exposed **read-only** under `/workspace/skills`.
- **Durability + delivery:** the **per-user Garage bucket `aura-<id>`** (already built via `garageadmin` + `identity_store`, not yet activated) is the durable store; access via a **forked, hardened S3 MCP**; `send_file` keeps delivering.

**Explicitly OUT of scope (v1):** whole-workspace snapshot/sync to Garage, FUSE-mounting the bucket as the working tree, host-bind to a Windows folder, the full LibreOffice suite/GUI (`-base`/`-draw`/desktop UI — headless `-writer`/`-calc`/`-impress` ARE in per Amendment #88.1), per-workspace quota/sweep, full sandbox enablement (deferred to the native-Linux mini-PC).

## 1. Why this is correct (evidence, not supposition)

- **The friction is real and diagnosed.** Conversation `019f83d0` (2026-07-21): the agent produced `/Agent.md.docx` but through ~30 turns — the file landed in the **ephemeral container root** (`os.Getwd()`), `docx` npm was missing → install + `NODE_PATH`/ESM-vs-CJS gymnastics, `file`/`xxd` missing (exit 127), and the operator has **no root access** so must always `send_file`. Toolchain inventory confirms the aura image lacks `pandoc`/`file`/`xxd`/LibreOffice, `docx` is not global, `NODE_PATH` is unset, `python-docx` is absent.
- **Three disjoint "workspace" notions today, no unified root:** (1) the process cwd where non-strict `shell_exec`/`fs_write`/`send_file` actually write (ephemeral container layer), (2) `AURA_RUN_DIR` (durable but conversation-keyed sidecars/spillover), (3) the per-identity sandbox box `/workspace` (strict-profile only). The `fs_*` tools are even registered with **no** `WorkspaceRoot` while `shell_exec`/`send_file` get one.
- **Garage/assets is already mature — and per-user buckets are built, not activated.** `garageadmin.BucketForIdentity` → `aura-<id>` + `CreateBucket`/`CreateKey`; `identity_store` resolves per-identity credentials, **crypto-isolated** (AES-256-GCM, KEK from `AURA_AUTHULA_SECRET`), **fail-closed**; `send_file` already ingests delivered files into the identity's bucket (Assets seam); `objectstore` has full S3 (presign/get/put/list/delete). But `aura.identity_object_store` is **empty** and only the shared `aura-assets` bucket exists → everyone (incl. the operator) is on the shared bucket today.
- **Industrial pattern (2026 survey + leaked system prompts).** A single **fixed workspace root** (`/home/ubuntu`, `/app`), a **pre-installed toolchain**, persistence via a **volume / suspend-resume**, and — universally — **local FS for the working tree, object store for durability** (Daytona/E2B/Vercel/Windmill; Manus/Devin/Emergent/Replit/Orchids). **Nobody runs the working tree on S3** (no `npm install`/venv/`node` against object storage; FUSE mounts are slow/fragile, worse on Docker Desktop).
- **The chosen S3 MCP is proven against Garage this session.** `txn2/mcp-s3` (Apache-2.0, Go) mounted as a docker stdio server in Claude Code → `claude mcp list` = **✔ Connected**; a `list_buckets` → `aura-assets`, `put_object` (size 15/etag), `get_object` (separate session), `presign_url`, `delete_object` round-trip all succeeded against `garage:3900` (path-style, custom endpoint).

## 2. Design

### 2.1 Local working area (`/workspace`)
- New Docker **named volume `aura-workspace`** → `/workspace` on the `aura` service. Layout: `scripts/`, `artifacts/` (staging pre-delivery), `.toolchain/` (node_modules + python venv), `skills/` (RO symlink to the skills export), `scratch/`.
- New env **`AURA_WORKSPACE_DIR`** (default `/workspace` in the container; `~/.aura/workspace` code default for CLI/dev) — convention `AURA_<DOMAIN>_<UNIT>`.
- `shell_exec`, `fs_read/write/edit/glob/grep`, and `send_file` take `WorkspaceRoot = AURA_WORKSPACE_DIR`, **fixing** the current `fs_*` no-WorkspaceRoot inconsistency in `serve_dispatch.go`. `Runner.workspace` (the per-turn "Working directory" hint) → `/workspace`. Same path in strict + non-strict (forward-compat with the box).

### 2.2 Toolchain pre-bake (aura Dockerfile)
- OS packages: `pandoc`, `file`, `xxd`, `poppler-utils` (`pdftoppm`/`pdftotext`), `qpdf`, `pdftk`, `tesseract-ocr`, `fonts-liberation` (Arial/Times-metric fonts for faithful render), and **LibreOffice headless modules** `libreoffice-writer`/`-calc`/`-impress` (the shared `soffice` binary the docx/pptx/xlsx skills call through `scripts/office/soffice.py`; full GUI/`-base`/`-draw` excluded). npm **global**: `docx` + `pptxgenjs` with `NODE_PATH` → global `node_modules`. pip: `python-docx`, `python-pptx`, `openpyxl`, `xlsxwriter`, `pandas`, `pypdf`, `pdfplumber`, `reportlab`, `pdf2image`, `pytesseract`, `Pillow`, `defusedxml`, `lxml`, `markitdown[pptx]` (all `--break-system-packages`, PEP-668). Extra installs land in `/workspace/.toolchain` (persistent). The 37A npm/pip/uv caches stay. **Sizing to the installed skills** (`docx`/`pptx`/`xlsx`/`pdf`, confirmed present in `/var/lib/aura/skills`) — Amendment #88.1.

### 2.3 Per-user Garage bucket (activate the built infra)
- Provision `aura-<id>` bucket + scoped key per identity at provisioning/first use (`garageadmin.CreateBucket`+`CreateKey` → encrypted `identity_object_store` row). The `local` principal keeps the shared `aura-assets` bucket (D-11). Fail-closed, exactly as `identity_store` already enforces.

### 2.4 Forked, hardened S3 MCP (`aura-mcp-s3`)
- **Fork `txn2/mcp-s3`** (Apache-2.0, Go) → `docker/mcp-s3/`, built as `aura-mcp-s3:local` (containerized, mirroring `docker/mcp-neo4j-cypher/` — no host install). Modifications:
  - **Hard-scope to ONE bucket** — override/ignore the per-call `bucket` arg so the server can only touch its configured bucket (defense-in-depth over the scoped key; strengthens MUSR isolation).
  - **Binary/file-aware** — add `put_file` (local path → binary-safe upload with content-type) and get-to-local-path, because docx/xlsx/pdf are binary and live as files in `/workspace` (upstream `put_object` takes a string `content`).
  - **Prefix confinement** to `workspace/`.
  - **Trim the tool surface** — drop `list_buckets` and cross-bucket `copy` (unneeded when hard-scoped).
  - Optional `ws_*` vocabulary (cosmetic). Read-write (`MCP_S3_EXT_READONLY=false`) but scoped.
- Mounted **per-identity** via `~/.aura/mcp/<id>/servers.json` with that identity's endpoint + bucket + scoped key (from `identity_store`). Isolation = scoped key **and** hard-scoped bucket.

### 2.5 Delivery
- `send_file` unchanged (ingests to the bucket + delivers to the channel); presigned URLs for authenticated web download (37A). Prompt doctrine: put deliverables in `artifacts/` and deliver with `send_file`; the toolchain is pre-installed (**do not reinstall** docx/python-docx/pandoc); your durable store is your bucket via the s3 tools.

## 3. Proposed PRD Amendment #88 (draft — ratify before code; next free number verified = 88)

> **Amendment #88 (2026-07-21): Dedicated persistent workspace + per-user object store.**
> Aura gains a fixed local working root **`/workspace`** (env `AURA_WORKSPACE_DIR`, backed by the `aura-workspace` volume) with a pre-baked document/data toolchain (`docx`, `python-docx`, `openpyxl`, `pandas`, `pandoc`, `file`, `xxd`; `NODE_PATH` set) and a persistent `/workspace/.toolchain` for extra installs; `shell_exec`, the `fs_*` tools, and `send_file` default their working root to it (same path as the sandbox box `/workspace`). Durable per-user storage uses the already-built per-identity Garage bucket **`aura-<id>`** (`garageadmin` + `identity_store`), activated by provisioning a bucket + scoped key per identity; the `local` identity keeps the shared `aura-assets` bucket. The agent accesses its bucket through a **forked, hardened S3 MCP** (`aura-mcp-s3`, from `txn2/mcp-s3`, Apache-2.0), containerized and mounted per-identity with the identity's scoped key + bucket, hard-scoped to that single bucket and binary/file-aware. `send_file` continues to deliver + ingest artifacts; web download via presigned URLs. Out of scope: whole-workspace S3 sync, FUSE mount, LibreOffice, quota/sweep, full sandbox enablement. Rationale + evidence + plan: `docs/superpowers/specs/2026-07-21-aura-dedicated-workspace-garage-design.md`.

## 4. Implementation plan (atomic commits — detailed by writing-plans)

1. **Workspace config + volume + wiring** — `AURA_WORKSPACE_DIR` + `aura-workspace` volume + layout bootstrap + `WorkspaceRoot` on `shell_exec`/`fs_*`/`send_file` + the per-turn hint + prompt doctrine. Daemon-free unit tests (default resolution, WorkspaceRoot fencing, fs_* consistency).
2. **Toolchain pre-bake** — aura Dockerfile (pandoc/file/xxd + npm `docx`/`NODE_PATH` + pip python-docx/openpyxl/pandas) + boot smoke (docx `require`, python-docx import, pandoc/file/xxd present). **[Amendment #88.1 expands this to Task 2b — LibreOffice headless + poppler/qpdf/pdftk/tesseract/fonts-liberation + pptxgenjs + the pptx/pdf/excel pip stack; see §0/§2.2/§5 for the full set.]**
3. **Activate per-user Garage buckets** — provisioning wiring (`garageadmin` CreateBucket+CreateKey → `identity_store` row) + tests (fail-closed, per-identity isolation, `local`→shared).
4. **Fork `aura-mcp-s3`** — `docker/mcp-s3/` + hardening mods (bucket hard-scope, `put_file`/get-to-path binary, prefix, trim) + per-identity MCP mount wiring + tests (containerized, per `[[containerize-never-install-mcp-on-host]]`).
5. **Prompt/doctrine + E2E** — re-run the docx scenario: zero reinstall, file in `/workspace/artifacts`, persists across `docker compose recreate`, durable in the per-user bucket, delivered via `send_file`. Score >9.8 real scenario.

## 5. Risks / carried

- **Image size** +~700 MB–1.1 GB from the toolchain (Amendment #88.1: LibreOffice headless `-writer`/`-calc`/`-impress` + tesseract + poppler + the pip stack — accepted for zero-friction docx/pptx/xlsx/pdf skill support; full GUI suite kept out to cap the growth).
- **Per-identity MCP process fan-out** at scale (one `aura-mcp-s3` process per active identity) — fine for single-operator now; flag for MUSR scale (a future connection-name multiplex or a shared server with runtime identity scoping could replace it).
- **S3 ≠ filesystem** — the working tree stays local; the bucket is the durable store only (no FUSE).
- **Secret handling** — per-identity S3 keys are encrypted at rest (`identity_store`); the Claude Code validation mount wrote a Garage key in plaintext into `.claude.json` (dev-only, to be scrubbed).
- **Binary round-trip correctness** in the fork (docx bytes must survive put→get) — covered by the E2E.

## 6. Sources

Live stack (docx conv `019f83d0`, toolchain inventory, Garage `ListBuckets`, empty `identity_object_store`) · Aura workspace-surface map · D:/tmp system-prompts digest (Manus/Devin/Emergent/Replit/Orchids) · online 2026 sandbox survey (Daytona/E2B/Modal/Vercel/Windmill) · `txn2/mcp-s3` (Apache-2.0) proven against Garage this session (mounted `garage-s3`, ✔ Connected + round-trip).
