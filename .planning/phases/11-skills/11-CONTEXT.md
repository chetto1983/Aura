# Phase 11: Skills - Context

**Gathered:** 2026-06-05
**Status:** Ready for planning — **the mandatory skills.sh spike is DONE (spikes 003-010, see D-15 + Spike Evidence below)**
**Method:** Research-grounded discussion (Phase 8/9/10 playbook) — 4 parallel researcher passes (2026 industrial skill patterns + agentskills.io spec, D:/tmp curated-source scan, skills CLI/registry ground-truth probe, always-on injection-point deep-dive) + repeated "self-test on Claude Code" verdicts requested by the user. 11 areas, 35+ decisions across ~30 interactive turns. The product North Star is the **xlsx E2E scenario** (see `<specifics>`): Aura must autonomously discover, gate-install, and use a skills.sh skill to produce a real Excel artifact.

<domain>
## Phase Boundary

Deliver CAP-07 + CAP-08 — the self-extension system. `internal/skills/` is **greenfield** (zero skill code in the repo). Builds: loader (multi-root FS scan + TTL cache 1s + parse-only validation), validator (NFKC + Unicode blocklist + 10K fuzz), writer (atomic pending→active + Postgres append-only audit, migration **0010**, floor is 0009), installer (skills.sh integration — **transport decided by a mandatory pre-planning spike**), catalog browse (**default-ON**, JSON API not HTML scrape), executable snippets v1 (by-path execution via the shipped `sandbox_exec`/sandbox-agent, ro `/skills` mount, TTL sweep as a **scheduler TaskKind**), ONE model-facing `skill` tool (ActionRouter), the **`messages[1]` always-on block** (the seam Phase 14 Agent.md will share), and a Wave-0 PRD-amendment commit fixing 4 confirmed staleness classes.

**PRD §Slice 7 is STALE in 4 places (scout-confirmed, amendment mandatory before code):**
1. Sandbox seam: PRD's `sandbox.Runner.Execute(language, code, args_stdin, SessionID)` is dead — shipped surface is `tools.SandboxExec` → sandbox-agent HTTP `:2468` (2026-06-03 pivot, amendment #44/D-15b).
2. Migration numbers: PRD `0007_skill_audit`/`0012_snippet_runs` → actual floor `0009_scheduler`; skills land at **0010+**.
3. Tool names: PRD's dotted `skill.list`/`skill.create` violate OpenAI-wire `^[a-zA-Z0-9_-]+$` (DeepSeek constraint).
4. "Skills enter the system prompt" rationale collides with the validated byte-stable `messages[0]` invariant (CAP-04) — superseded by the messages[1] design (D-06/D-07).

**Out of scope (confirmed):** 7f pattern-analyzer cross-conv auto-suggest (SKILL-V2-01, v1.x — but D-26's headless pending+alert path is the seam it will reuse), Neo4j HNSW semantic skill discovery (Phase 15 era; v1 discovery = BM25), skill versioning machinery (D-23), public marketplace/registry hosting, multi-user skill sharing, runtime `inputs_schema` enforcement (D-20), edit-on-approve ("approva con modifiche"), **the gVisor `runsc` overlay + seccomp re-tightening (D-38 — a Phase-8 sandbox-wide regression, tracked there; Phase 11 only DEPENDS on the portable hardening floor existing, it does not build the prod isolation tier).**

</domain>

<decisions>
## Implementation Decisions

### Tool surface (Area 1)
- **D-01 ONE `skill` tool via ActionRouter** (`internal/agent/tools/action.go` — built in Phase 10 explicitly for this reuse). Actions: `list|info|use|create|update|delete|install|catalog|restore|archive`. Phase-10 D-10 schema discipline: top-level `required=["action"]` ONLY, per-action fields in descriptions, no root `oneOf`. Supersedes the PRD's 7-11 separate `skill_*` tool files.
- **D-02 Full mutation set stays model-facing, gated** (create|update|delete|install) — self-extension is the slice's point; mutations land in `pending/` with `scoring.ComputeSkillTier` (shipped Phase 8, unwired — Phase 11 is its designated consumer) + ask_user governance per PRD Area #5 flow.
- **D-03 NO model-facing `approve` action** (Claude Code self-test: the model cannot approve its own actions; permission is harness-level). Activation ONLY via ask_user resume (human answered) or `aura skills approve` CLI. `skill.approve` drops from the PRD tool surface (amendment). Closes the self-approval loophole structurally.
- **D-04 NO bespoke `run` action** (Claude Code self-test: bundled scripts execute by path via the generic execution tool; code never re-enters context). Snippets materialize as files; `action=use` returns instructions + the in-sandbox path; the model executes via `sandbox_exec {command: python, args: ["/skills/<name>.py", ...]}`. CLI `aura skills snippet exec` is the deterministic operator path that stamps usage metadata. Model-run usage stamping via a `sandbox_exec` argv `/skills`-path-prefix hook = planner discretion (argv is structured ground truth, NOT NL regex).
- **D-05 `skill` tool is NON-deferred** (Claude Code parity) — always visible; the sandbox_exec schema-blindness lesson applies to a 10-action enum. Builtins: multi-root precedence + Go `embed.FS` + codex-style fingerprint materialization wired now; embedded content = **skill-creator meta-skill only** (D-31). Global FS (`~/.aura/skills/`), identity only in audit rows (actor_id/identity_id) — multi-user partitions via DB later, not FS. Update flow: pending while old version serves; gate shows unified diff; reject discards pending.

### Discovery + prompt injection (Area 2) — the cache-safe architecture
- **D-06 Available-skills manifest lives in the `skill` tool's Description** — generated at registry build (08.1 D-09 dynamic-description precedent; Claude Code docs parity). Name + trigger-rich description per skill. Turn-stable; `messages[0]` untouched; a skill add/remove busts the tools-prefix cache once on the next turn (rare, accepted). Plus ONE static byte-stable mechanism sentence in `messages[0]` ("skills exist; the skill tool lists/applies them") — mechanism-not-enumeration (D-08/D-09 prompt discipline + `feedback_agent_must_know_tools_exist`).
- **D-07 Two-tier with `always: true` frontmatter; always bodies render into ONE user-role block at `messages[1]`** — research-verified convergent pattern (Claude Code CLAUDE.md = user message after system prompt; Codex AGENTS.md + skills = user-role fragments; nanobot's system-prompt concat is the documented outlier anti-pattern; OpenAI-wire prefix caching: a messages[1] change preserves the messages[0]+tools prefix). This block is THE seam Phase 14 Agent.md will share (profile first, always-skills after). PRD haiku smoke passes as written. **FLAGGED CONSTRAINT: the Phase-4 L2.5 evictor must protect the messages[1] block like it protects system L0** — otherwise a long conversation silently evicts haiku mode.
- **D-08 `action=use` returns the body wrapped in an authority frame** ("Follow these skill instructions for the current task: …") via the normal ToolResult path (preview+sidecar >2KiB). `info` stays as the plain-read variant (inspection/diffing). Invocation means ADOPT (CC parity).
- **D-09 Manifest scale: cap + BM25 overflow.** Description block lists skills up to `AURA_SKILL_MANIFEST_CAP_BYTES` (~8k, Codex parity); past the cap it ends with "N more — search with skill action=list {query}"; `list` is BM25-ranked over name+description (reuse the shipped 08.1 ranker). PRD "ALL skills listed even 100+" acceptance amended.
- **D-10 `always:true` governance:** model CAN propose it in create/update but the gate flags it loudly ("⚠ ALWAYS-ON: steers every future turn"); `install` STRIPS the flag from third-party skills unconditionally (re-enable only via `aura skills always <name>`); audit records the flag. **No count cap v1** (CC self-test: CLAUDE.md has no enforced cap; the human gate is the governor) — bounds = per-skill 32KiB body cap + `aura skills list` shows the always tier with rendered byte size + L2 budget ladder safety net.

### Catalog + installer (Area 3) — spike-gated
- **D-11 HTML scrape is DEAD.** skills.sh exposes a JSON search endpoint (`GET /api/search?q=` → `{skills:[{id,skillId,name,installs,source}]}` — probed live 2026-06-05, CLI v1.5.10 `find` hits the same backend). PRD 7b's `catalogItemRE` design amended away. Caveat: it's the CLI's internal endpoint (vercel-labs/skills#426 — no public API yet).
- **D-12 Catalog browse default-ON** — amendment #14 FLIPPED (its rationale was the scrape's attack surface; browse is now read-only JSON). The real gate moves to install: RISKY tier + ask_user + validator + flag-stripping. `aura skills disable-catalog` stays as the default-deny escape hatch. ROADMAP SC#5 re-specced by the amendment. Model-facing `action=catalog {query}` enabled out of the box.
- **D-13 Install posture: tolerate, surface in gate.** Accept agentskills.io-spec optional frontmatter (`license`/`compatibility`/`metadata`/`allowed-tools` — inert for Aura, spec interop); the approval gate SURFACES red flags (`metadata.*.install[]` blocks, tool wildcards, bundled executable count); bundled scripts only ever run via sandbox_exec (containment); `always` stripped (D-10); body injection-blocklist applies (D-27); `skills-lock.json` `computedHash` pinned at install (TOFU).
- **D-14 Install-flow ground truth (probed):** `skills add` = git clone + copy/symlink, **no npm postinstall ever runs** — `--ignore-scripts` is a no-op in v1.5.10 (PRD's P0 mitigation is moot; amendment notes it). `--skill` must be repeated (CSV broken, memory-confirmed). `--copy` for hermetic dirs. The CLI adds little beyond clone+copy+hash — native shallow git-clone (dropping the node/npx host dep) is a live option for the spike to settle.
- **D-15 MANDATORY pre-planning `/gsd-spike` — DONE 2026-06-05 (spikes 003-010).** Verdicts locked the following (see `.planning/spikes/003..010` + the Spike Evidence block below):
  - **Catalog transport = skills.sh `/api/search` JSON, lax-decode** (003). HTML scrape DEAD. Schema drifts (`isDuplicate`/`count`) → never `DisallowUnknownFields`; guard empty queries (400); rank by `installs`; multi-word queries hit server-side semantic search. npx `find` is fallback only.
  - **Installer = native Go clone, NOT npx** (004a vs 004b: native WINS — bit-identical, 3× faster, drops the node dep). `git clone --depth 1 --single-branch -c core.autocrlf=false` (LookPath fixed-argv) + symlink-stripping copy + **Aura's OWN canonical hash** (byte-sorted (relPath,bytes) sha256). The upstream `skills-lock.json` `computedHash` is locale/platform-sensitive — do NOT interop with it.
  - **ro `/skills` mount works** on Docker Desktop (005) — by-path exec, immediate live visibility, ro enforced, de-materialization clean; the Phase-8 `device=` failure class does NOT apply to compose bind mounts. **Symlinks resolve in-container → writer/installer MUST strip them at materialization** (reuse Phase-4 `ScanOrphans` Lstat-no-follow). **Always invoke by interpreter+path (`python3 /skills/...`), never the exec bit** (Docker Desktop masks 777; native Linux won't).
  - **North-Star ingredients all live** (006): real anthropics/skills→xlsx installs, frontmatter tolerant-parses (**needs a REAL YAML lib** — its `description` is a double-quoted scalar with escaped quotes; + CRLF normalization), bundled scripts run by path, real `.xlsx` produced+verified.

### 7e snippets on sandbox-agent (Area 4)
- **D-16 TTL sweep = scheduler TaskKind** (`skill_ttl_sweep`, seeded daily like the backup tasks — Phase-10 machinery free: persistence/HA/missed-catch-up/audit). PRD `ttl_sweeper.go` goroutine amended away. Sweep threshold `AURA_SKILL_SNIPPET_TTL_DAYS` (90).
- **D-17 Read-only `/skills` mount:** compose adds host export dir → `/skills` ro in-container; writer/installer materialize executable snippet files **on activation only** (pending/archived never materialize; archive/delete de-materializes — mount in lockstep with loader state); stable paths handed out by `action=use`; immutable from inside the sandbox.
- **D-18 Snippet outputs ride the existing conversation-scoped workspace purge cascade** (Phase 8 ConversationCleaner) — PRD OQ4 ratified, zero new machinery.
- **D-19 Usage/archive state: sidecar JSON per skill** (status/last_used_at/use_count — the ONE live-state source, atomic-write) **+ `snippet_runs` DB table for per-run forensics only**. The PRD's `skill_audit` ALTER columns (last_used_at/use_count) DROP (amendment). TTL sweep scans sidecars (small N).
- **D-20 Snippet frontmatter: docs-only except tier-relevant.** `language` (required enum `python|shell|js` — all in the sandbox image) + `description` enforced; `inputs_schema`/`outputs_desc`/`deps`/`tags` = OPTIONAL documentation rendered by `use` (validator tolerates, never enforces); `needs_network`/`needs_workspace` kept and surfaced in the SAVE-time risk gate. No runtime arg validation v1.
- **D-21 Snippet discovery v1 = the D-09 BM25 `list` path.** Neo4j HNSW semantic discovery deferred (7f/Phase 15).
- **D-23 Versioning: implicit via audit** (PRD OQ3 ratified) — content_hash in every audit row is the recovery path; update replaces atomically with diff in gate; no version machinery (no peer ships one).
- **D-36 Dep strategy is a PLANNER CHOICE, not a forced bake (spike 006→007 correction).** Spike 006's "egressless → must bake deps" premise was UNPROBED and WRONG: the sandbox already has pip + full internet egress on the dev stack (the `aura-sandbox-local` `masquerade:false` bridge is still NAT'd by Docker Desktop vpnkit). Spike 007 proved on-demand is viable — `uv` (static binary, no `curl|sh`) installs openpyxl in 292ms, pandas+numpy in 3.1s into per-skill venvs. Three models the planner picks from: (1) **build-time bake** (hash-pinned curated set in `docker/sandbox-agent/Dockerfile`; offline-resilient, deterministic, image grows), (2) **on-demand uv** (bake uv as tooling; skills declare `deps:` which then becomes LOAD-BEARING not docs-only — supersedes D-20's docs-only stance for that field; needs egress), (3) **hybrid** (bake the common heavy set, uv the long tail — recommended lean). The xlsx North-Star needs `openpyxl, defusedxml, lxml, validators` (its bundled scripts' import set, characterized in spike 006) — whichever model ships, those must resolve.
- **D-37 Sandbox egress posture is an EXPLICIT planner decision, not Docker Desktop's accident (spike 007/009).** On-demand deps (D-36 model 2/3) and any `needs_network` snippet require controllable egress. Spike 009 proved an ~80-LOC host forward-proxy with a hostname-CONNECT allowlist works, BUT `HTTP(S)_PROXY` env is **advisory on Docker Desktop** (a process that ignores it egresses directly via vpnkit NAT) — **enforceable only on a native-Linux non-masquerading bridge** where the proxy is the container's ONLY route. The Phase-8 D-08 host forward-proxy + pypi allowlist was specced then LOST in the sandbox-agent pivot. `needs_network:true` → route through the allowlisted proxy (per-skill or global allowlist) at the RISKY tier; `needs_network:false` → no proxy env + (prod) no route = truly offline. **This is shared with Phase-8 sandbox hardening** (see D-38) — the planner decides the egress boundary here for skills and flags the Phase-8 regression.
- **D-38 Sandbox hardening floor is a Phase-11 dependency; gVisor is the prod/CI tier (spikes 008/009/010).** Model-authored skill code makes the sandbox's isolation gap load-bearing. Spike-validated:
  - **Token auth (008): VALIDATED, ~5 LOC.** `--token` makes `:2468` enforce a bearer (401 unauth, including `/v1/health` → healthcheck must carry it; 200 + exec on correct token). Today Aura runs `--no-token` (unauthenticated exec on loopback). Wire `internal/sandboxagent.Client` to send `Authorization: Bearer` from a new `AURA_SANDBOX_AGENT_TOKEN` (gen at first boot, mirror `AURA_SETUP_TOKEN`). Cheap, do it.
  - **Portable floor (runs on Docker Desktop AND prod):** token (008) + tightened seccomp + `no-new-privileges` (already set) + read-only rootfs + the D-37 egress allowlist. This is what Phase 11 can rely on everywhere.
  - **gVisor `runsc` (010): the workload survives gVisor** (`runsc do python3` with tempfile/urandom/sha256 ran clean) **but Docker Desktop CANNOT host runsc** ("unknown runtime") — it's a **native-Linux/CI/prod-only** tier (matches Phase-8 gVisor-primary-x86 + the CAP-02 Docker Desktop blocker memory). Restore the Phase-8 `compose.gvisor.yaml` overlay (lost in the pivot) applied on native-Linux/CI only; CI gates it via DinD+runsc, dev asserts the portable floor.
  - **Scope note:** token + egress are Phase-11-relevant (skills execute model code) and decided here; the gVisor overlay + seccomp re-tightening are a **Phase-8 sandbox-wide regression** broader than skills — track against Phase 8, do not let it bloat Phase 11's plan. The 7e executor only DEPENDS on the portable floor being present.

### Validator + governance (Areas 6/9/10)
- **D-27 Injection blocklist: hard for model, override for operator.** Model-authored create/update: NFKC + literal blocklist hard-reject, no escape. Install/CLI: same check, but the operator can pass `--allow-blocklisted` after the gate shows exactly which sequences matched and where (skills *documenting* LLM prompt formats legitimately contain blocklisted tokens — part of the real skills.sh corpus). Audit records the override (`blocklist_override` bool).
- **D-28 Loader validates parse + structure only** (frontmatter, name regex, size; skip+log invalid per PRD). Blocklist enforced ONLY at write boundaries. Disk = operator-trusted (CC parity); no override-marker machinery. Fuzz target = `FuzzSkillValidator`, 10K NFKC/Unicode mutations of blocklist patterns (ROADMAP SC#3). `SanitizeName` single-chokepoint + static-analysis test per PRD (unchanged).
- **D-26 Headless contexts (cron agent_jobs, swarm workers) MAY attempt mutations** — no registry stripping. A headless mutation can never self-activate (no model approve D-03 + ask_user auto-reject Phase-10 D-25 → `pending/` + Notifier IMMEDIATE alert + audit `gate_taken=false`). "Aura proposed skill X overnight — approve?" is a feature; it's the exact seam 7f auto-suggest reuses.
- **D-29 Audit coherence matrix RATIFIED** (replaces the PRD's three-way equivalence; planner writes the exact 0010 CHECK SQL):

  | Event | approval_source | paused_state_token | gate_recommended | gate_taken |
  |---|---|---|---|---|
  | Mutation written to pending (gate not yet exercised) | **NULL** | NULL | true | false |
  | User approved via ask_user resume | `ask_user` | NOT NULL | true | true |
  | User rejected via ask_user resume | `ask_user` | NOT NULL | true | true |
  | Operator CLI approve / `--allow-blocklisted` | `cli` (+`blocklist_override`) | NULL | true | true |
  | System (TTL `auto_archive`, `cleanup_pending_stale`) | `auto` | NULL | false | true |

  Append-only BEFORE UPDATE/DELETE trigger + BEFORE TRUNCATE statement trigger + `aura_app` role separation stay exactly per PRD (Pitfall #6 belt-and-suspenders).

### Frontmatter format (research-locked)
- **D-30 agentskills.io spec compat:** `name` (≤64, `[a-z0-9-]`, must match dir name) + trigger-rich `description` (≤1024) required; `license`/`compatibility`/`metadata`/`allowed-tools` tolerated (inert); Aura extensions: `always: true` (D-07/D-10), `type: instruction|snippet` (default instruction) + snippet fields (D-20). Parse with a real YAML lib (picobot's hand-roll is the anti-reference). NO `when_to_use`/`tools` fields (PRD OQ1 stands — description incorporates the when-to-use, confirmed by the spec).
- **D-31 Embedded builtin = skill-creator meta-skill only** (codex pattern adapted): teaches spec-compliant authoring — trigger-rich descriptions, when `always:true` is appropriate, snippet format. `find-skills` is NOT embedded — it arrives via the North-Star E2E install flow.

### Process (Areas 5/7/11)
- **D-32 Sub-slice commits keep PRD lettering, amended content:** Wave-0 doc-only PRD-amendment plan 11-01 FIRST (05-01/08-01/09-01/10-01 precedent), then 7a (loader+validator+paths+read actions+manifest-in-description) → 7b (catalog API client, post-spike) → 7c (writer+audit 0010+governance+always/messages[1] block) → 7d (installer) → 7e (snippets+mount+TaskKind). Each atomic + smoke green.
- **D-33 Wave-0 amendment package covers:** sandbox-seam rewrite (§1 staleness), migration renumber 0010+, dotted names → one `skill` tool (D-01), skills-in-system-prompt → messages[1] (D-07), `skill.approve` cut (D-03), `ttl_sweeper.go` → TaskKind (D-16), catalog scrape → JSON API + native clone (D-11/D-14/D-15), amendment #14 flip + SC#5 re-spec (D-12), metadata consolidation (D-19), audit matrix (D-29), manifest-packing acceptance (D-09), env catalog (D-34), **dep-strategy trichotomy + the corrected "not forced bake" framing (D-36), sandbox egress posture as explicit decision (D-37), and the sandbox token-auth + portable-hardening-floor dependency (D-38; gVisor overlay cross-referenced to Phase 8, not owned here).**
- **D-34 Env vars RATIFIED** (names/defaults refinable by planner in convention; EXPORT_DIR/CATALOG_URL final shape spike-informed): `AURA_SKILLS_DIR` (~/.aura/skills), `AURA_SKILL_BODY_CAP_BYTES` (32768), `AURA_SKILL_INJECTION_BLOCKLIST` (builtin list), `AURA_SKILL_MANIFEST_CAP_BYTES` (~8192), `AURA_SKILL_SNIPPET_TTL_DAYS` (90), `AURA_SKILL_EXPORT_DIR` (~/.aura/skills/export), `AURA_SKILL_CATALOG_URL` (https://www.skills.sh), `AURA_SKILL_INSTALL_TIMEOUT_SEC` (90). Pending-TTL 24h stays a constant (PRD closed OQ5).
- **D-35 E2E dual gate, Phase-9 style:** cot_eval live tier (OPENROUTER-gated, operator-run, NOT CI) — hard floor = ground-truth assertions (tool_use sequence: catalog→ask_user→install→sandbox_exec; .xlsx exists/opens/contains today's data — `feedback_probe_must_verify_artifact_not_reply`) + judge rubric ≥90% (autonomous capability-gap recognition, install prudence, output quality). Numbers → `docs/aura-quality-snapshot.md`. Plus 2 smaller smokes: haiku flow (create→approve→messages[1]→next-turn behavior) + snippet save→by-path reuse.

### Claude's Discretion
- Exact ActionRouter handler split across files (≤600 LOC discipline), per-action description wording, the authority-frame literal for `use` (load-bearing-literal test per the `finalizeNudge` pattern).
- sandbox_exec argv `/skills`-prefix usage-stamping hook (D-04) — design + whether it ships in 7e or as a fast-follow.
- BM25 reuse shape for skill `list` (corpus build, ranking params) — mirror the 08.1 implementation.
- Migration split across sub-slice commits (one 0010 vs 0010+0011), exact sqlc query set, store adapter (copy the canonical 04-02 pattern).
- messages[1] block exact rendering (header literal, skill ordering, byte-stability discipline) + the L2.5 evictor protection mechanics.
- skill-creator builtin skill content authoring.
- `aura skills` CLI subcommand set rendering (hand-rolled switch per runDB/runIdentity/runTask precedent — no cobra).
- Notifier route for gate-skipped alerts (reuse Phase-10 composite chain + quiet-hours semantics as-is).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope (in-repo)
- `prd.md` §"Slice 7 — Skills" (lines ~2112-2330) + §"Slice 7e-core" (lines ~2331-2571) — truth-source for acceptance/file-targets/governance, **STALE in 4 confirmed places** (see Phase Boundary); the D-33 Wave-0 amendment supersedes those items.
- `.planning/ROADMAP.md` §"Phase 11: Skills" (lines ~338-352) — goal + 5 success criteria (SC#5 re-specced per D-12; SC#1 `npx skills add --ignore-scripts` wording amended per D-14).
- `.planning/REQUIREMENTS.md` CAP-07 (line 38) + CAP-08 (line 39).
- `prd.md` §Risk-Based Governance (lines ~4459+) — the gate pipeline; §"Caps & Limits" env catalog (D-34 additions land there).

### Spike evidence (session 2, 2026-06-05 — the D-15 mandate, all live-validated)
**Downstream agents MUST read these spike READMEs — they are the ground truth behind D-11/D-14/D-15/D-17/D-36/D-37/D-38.**
- `.planning/spikes/003-skills-sh-search-api/README.md` — `/api/search` lax-decode contract; schema drift; semantic multi-word; both North-Star targets top-ranked.
- `.planning/spikes/004a-install-npx-cli/` + `004b-install-native-clone/README.md` — **native clone WINS** (bit-identical, 3× faster, node-dep droppable); the canonical-hash algorithm + the autocrlf/locale lockfile-hash trap.
- `.planning/spikes/005-skills-ro-mount/README.md` + `compose.skills-mount.yaml` — ro `/skills` mount proof; symlinks-resolve-in-container → strip at materialization; interpreter-not-exec-bit rule.
- `.planning/spikes/006-xlsx-skill-dry-run/README.md` + `Dockerfile` — North-Star ingredients live; real-YAML-parser + CRLF need; the xlsx dep set (`openpyxl/defusedxml/lxml/validators`).
- `.planning/spikes/007-uv-on-demand-deps/README.md` + `Dockerfile` — **overturns 006's must-bake premise**; uv timings; the dep-strategy trichotomy (D-36).
- `.planning/spikes/008-sandbox-token-auth/README.md` + `compose.token.yaml` — bearer auth VALIDATED (401/200), client-wiring obligation (D-38).
- `.planning/spikes/009-sandbox-egress-allowlist/README.md` + `proxy/main.go` — ~80-LOC CONNECT-allowlist proxy; advisory-on-Docker-Desktop / enforced-on-native-Linux (D-37).
- `.planning/spikes/010-sandbox-gvisor-runsc/README.md` — gVisor workload survives, Docker-Desktop-can't-host-runsc → CI/prod tier (D-38).
- `.planning/spikes/MANIFEST.md` §"Phase-11 requirements" — the binding one-line summary of all of the above.
- **Live substrate proof (motivation, not a spike):** a real `aura chat` turn (web_search→SearXNG + sandbox_exec→openpyxl) produced a verified `.xlsx` of today's markets WITHOUT any skill — proving the substrate already delivers the North-Star and that Phase 11's skill layer is the **quality + reuse multiplier** (battle-tested methodology, source attribution, no per-run re-derivation), not a missing primitive.

### Shipped code this phase builds on (ground-truth read during scout)
- `internal/agent/tools/action.go` — ActionRouter (built for this reuse, doc comment explicit) + `internal/agent/tools/task.go` — the one-tool/action-enum + D-10 schema-discipline precedent.
- `internal/sandboxagent/client.go` — the exec client (D-38 adds an `Authorization: Bearer` header from `AURA_SANDBOX_AGENT_TOKEN`); `docker/sandbox-agent/Dockerfile` + `compose.yaml:90-118` (`aura-sandbox-agent` service + `aura-sandbox-local` net) — the D-36 bake / D-37 egress / D-38 gVisor-overlay targets.
- `internal/agent/tools/sandbox_exec.go` + `internal/sandboxagent/` — the ONLY execution seam (D-04/D-17); note its non-deferred schema-visibility lesson (comment at Spec()).
- `internal/scoring/scoring.go` `ComputeSkillTier` (line ~163) — shipped Phase 8, unwired; Phase 11 is its designated consumer.
- `internal/agent/tools/spec.go`/`manifest.go`/`search.go`/`bm25.go` — Registry/Deferred/BM25 mechanics (D-06 description generation, D-09 overflow); alphabetical manifest ordering is cache-load-bearing.
- `internal/agent/prompt.go` — byte-stable `SystemPrompt`; D-06's static mechanism sentence is the ONLY edit allowed (one-time, then frozen).
- `internal/agent/llm_agent_pause.go` + `internal/askuser/store.go` — ask_user approval/resume seam (D-03 resume handler = Writer.Activate + Invalidate + audit).
- `internal/conversations/context.go` — L1/L2/L2.5 ladder; **D-07's flagged constraint lands here** (protect messages[1]).
- `internal/identity/store.go` — canonical store pattern (D-A4-01) the audit store copies; `internal/db/migrations/0009_scheduler.up.sql` — migration floor.
- `internal/cron/` — TaskKind handler registration for D-16's `skill_ttl_sweep`; builtin-task seeding pattern (backup tasks).
- `cmd/aura/main.go` `buildBaseRegistry` (line ~97) — registry wiring for the `skill` tool; `cmd/aura/chat.go` `bootChat` — composition root.
- Pre-rewrite references (read at tag `pre-rewrite-2026-05-27`, pattern-only): `internal/skills/loader.go` (the 4-concern split target), `internal/agent/tools/registry/skill.go` (the 347-LOC anti-pattern D-01 kills).

### Industrial standard (external — research evidence)
- <https://agentskills.io/specification> — the open Agent Skills spec (D-30 frontmatter contract).
- <https://www.skills.sh/api/search?q=> — JSON search endpoint (probed live; D-11); <https://github.com/vercel-labs/skills> — CLI source (v1.5.10 verbs; install = clone+copy, no postinstall; `skills-lock.json` computedHash); <https://github.com/vercel-labs/skills/issues/426> — no-public-API caveat.
- <https://code.claude.com/docs/en/skills> + <https://code.claude.com/docs/en/memory> — Skill tool + memory injection (D-05/D-07 parity source).
- <https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills> — progressive disclosure 3-tier model.
- <https://developers.openai.com/codex/skills> — Codex skills (manifest cap ~8k precedent, D-09).
- North-Star skill targets: <https://www.skills.sh/vercel-labs/skills/find-skills> + <https://www.skills.sh/anthropics/skills/xlsx> (D-35 E2E).

### Curated peer sources (D:/tmp — mirror/avoid these patterns)
- `D:/tmp/system_prompts_leaks/Anthropic/claude-code.md` — L28-38 skills manifest in system-reminder, L839-877 Skill tool schema, L851-858 discovery rules (D-05/D-06/D-08).
- `D:/tmp/nanobot/nanobot/agent/skills.py` (L94-120) + `context.py` (L86-94 always-tier; L206-213 runtime-ctx-at-tail lesson) — two-tier progressive loading (D-07); its system-prompt concat = the cache anti-pattern.
- `D:/tmp/picobot/internal/agent/context.go` (L62-73) — eager full-body-in-system-prompt ANTI-PATTERN (do not copy); `skills/loader.go` — Go loader shape reference.
- `D:/tmp/codex/codex-rs/skills/src/lib.rs` — embed.FS+fingerprint builtin materialization (D-05); `skills/src/assets/samples/{skill-installer,skill-creator,imagegen}/SKILL.md` — installer flow + skill-creator content reference (D-31); `core/src/context/user_instructions.rs`+`skill_instructions.rs` — user-role fragment injection (D-07 evidence).
- `D:/tmp/assistant-ui/.claude/skills/tap/SKILL.md` — real frontmatter+body style (trigger-rich description).
- `D:/Aura/.claude/skills/{golang-testing,code-maturity-assessor,neo4j-cypher-skill}/SKILL.md` — the installed 56-skill corpus: real third-party frontmatter diversity (D-13's tolerate rationale; `metadata.openclaw.install[]` red-flag examples).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `ActionRouter` — D-01's dispatch core, shipped and tested; `task.go` is the copy-from template.
- `scoring.ComputeSkillTier` — shipped + unit-tested; create/update/install → RISKY, delete → DESTRUCTIVE.
- 08.1 BM25 ranker (`bm25.go`) — D-09's overflow search over the skill corpus.
- `sandbox_exec` + sandbox-agent stack (`make sandbox-up`) — D-04 execution; Phase-5 image already bakes openpyxl/pandas/etc. (xlsx E2E needs no image change).
- Phase-10 scheduler TaskKind machinery + builtin-task seeding — D-16's sweep host.
- Phase-10 composite Notifier (WhatsApp/mail/stdout + quiet hours) — gate-skipped IMMEDIATE alerts.
- ask_user pause/resume + `ResumeContext` + paused_states — D-03's approval channel (proxied_* columns already shipped).
- Canonical store pattern (identity 04-02 lineage) + `db.WithTx` — the audit store copies it; append-only trigger SQL patterns from PRD.
- `tools.NewResult` preview+sidecar — `use`/`info` body delivery (D-08).
- goleak TestMain + injectable clock + no-skip-as-green CI discipline — all tiers.

### Established Patterns
- Doc-only Wave-0 PRD-amendment plan (05-01/08-01/09-01/10-01) — D-33 is the fifth.
- Deferred-tool/manifest cache discipline — `skill` is non-deferred (D-05) but its Description must stay turn-stable; manifest alphabetical.
- `messages[0]` byte-stable invariant (CAP-04) — D-06/D-07 designed around it; ONE frozen mechanism sentence is the only system-prompt change ever.
- Load-bearing literal + asserting test (finalizeNudge pattern) — the `use` authority frame + the D-24-style anti-over-trigger description phrases.
- Spike-before-plan (Phase 9 spikes 001/002) — D-15.
- Risk-gate flow uniformity (Phase 10 task pending_approval) — skill mutations follow the same shape.

### Integration Points
- `cmd/aura/main.go` `buildBaseRegistry` — registers the `skill` tool (non-deferred) + skill-tool Description generation hook; `aura skills {list|info|create|update|delete|install|approve|always|audit|snippet save|snippet exec|restore|archive|disable-catalog}` CLI switch.
- `cmd/aura/chat.go` `bootChat` — loader init + builtin materialization + messages[1] block assembly seam (Runner-side).
- `internal/db/migrations/0010_skill_audit.{up,down}.sql` (+ optional 0011 for snippet_runs) + `internal/db/queries/skill_audit.sql` (sqlc).
- `compose.yaml` — sandbox-agent service gains the ro `/skills` mount (D-17; spike-verified).
- `internal/cron` handler registry — `skill_ttl_sweep` TaskKind + seeded task.
- `internal/conversations/context.go` — L2.5 evictor messages[1] protection (D-07 flagged constraint).

</code_context>

<specifics>
## Specific Ideas

- **THE xlsx North-Star E2E (user-authored acceptance lens):** with `find-skills` (vercel-labs/skills/find-skills) installed **via the real install flow**, the user asks (natural prompt, no "skill" mentioned): *"fammi un file Excel con il mercato di Yahoo Finance di oggi"*. Aura must BY ITSELF: recognize the capability gap → discover `anthropics/skills/xlsx` on skills.sh (catalog/find-skills path) → ask_user install approval → install + validate → use the skill (bundled Python scripts by-path in the sandbox) → pull today's data via the shipped web tools → produce the `.xlsx`. Ground truth: the file exists in the workspace, opens, contains today's data (artifact-not-reply assertion) + the tool_use sequence is asserted. This one scenario exercises: catalog browse, install governance, instruction-skill use, bundled-script execution, the ro mount, web-tools integration, artifact production.
- "Look how Claude Code works — self-test on yourself" was the user's repeated decision heuristic (Q3 deferred-status, Q4 run path, Q5 approve gate, always-tier bounds): when in doubt downstream, mirror the live Claude Code harness behavior, not just its docs.
- Research-first discipline: the user explicitly mandated searching 2026 patterns online + D:/tmp curated sources before deciding — and a spike before planning. Future similar phases deserve the same treatment.
- skills.sh is the chosen end-user repository — "easy install for end user" is a product goal, not a nice-to-have (browse default-ON flows from it).

</specifics>

<deferred>
## Deferred Ideas

- **7f pattern-analyzer cross-conv auto-suggest** → SKILL-V2-01 (v1.x) — D-26's headless pending+alert path is its ready-made seam.
- **Neo4j HNSW semantic skill discovery** → Phase 15 era (v1 = BM25; revisit when the memory subsystem ships embeddings infra).
- **Skill versioning / rollback machinery** (`aura skills rollback`, last-N bodies) → future; content_hash-in-audit is the v1 recovery path.
- **Snippet-output retention policy** (separate from conversation lifetime, e.g. ~/.aura/artifacts) → only if a real need appears.
- **Runtime `inputs_schema` enforcement** → re-evaluate if model-driven snippet misuse shows up in evals.
- **`/skill-name` slash-command invocation in channels** → Telegram/Phase 13 era (CC parity feature; needs a channel command surface).
- **Public API adoption for skills.sh** → when vercel-labs/skills#426 ships, replace the internal endpoint (the catalog client should isolate the transport for this swap).
- **Edit-on-approve ("approva con modifiche")** → out of scope per PRD; revisit with a richer approval UX (AG-UI era).

### Reviewed Todos (not folded)
None — `.planning/todos/pending/` is empty (todo.match-phase returned 0).

</deferred>

---

*Phase: 11-skills*
*Context gathered: 2026-06-05*
