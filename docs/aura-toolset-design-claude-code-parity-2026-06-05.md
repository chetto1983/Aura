# Aura toolset design — Claude Code parity study

**Date:** 2026-06-05
**Author:** Claude Code (Opus 4.8) session with Davide
**Status:** design study — decision locked by owner; informs a lean PRD amendment + one slice.

## Decision (owner, this session)

> **Aura gets a full terminal — the same one Claude Code has — living in a sandbox *home* that
> mounts the workspace folder and the skills folder. Whatever Claude Code can do, Aura does the same.
> No stupid security toys.**

The sandbox stops being a locked-down single-command HTTP box. It becomes Aura's *home*: a full,
persistent shell with the real folders present. The container boundary stays only because it's *where
Aura lives* (like the host is where Claude Code lives) — not as a cage. Every lockdown layer added on
top of it comes off.

## Thesis

> **Aura already has Claude Code's brain. It is missing Claude Code's hands.**

Aura has the entire *orchestration* layer (sub-agents, scheduling, skills, deferred-tool loading, HITL
questions, web, graph memory). What it lacks is the *native primitive* layer — the few in-process,
full-access tools that make Claude Code feel powerful. Aura replaced that whole layer with a single
`sandbox_exec` that HTTP-POSTs one command at a time into a Docker container scoped to `/workspace`,
behind token auth + egress allowlists. That is the overcomplication.

## Evidence — what Claude Code actually does with the terminal

Captured live on this host (`NB-TECNICI-MARDAV`, Windows 11) in one turn, ~8 commands, every one
**sub-second, in-process, on the real host, zero HTTP to any sidecar**. The only network call in a
turn is the one to the model.

| Command class | Live result | Capability proven |
|---|---|---|
| `whoami` / OS / `$PID` | `nb-tecnici-mard\davide`, Win 11 Pro | runs **as the operator, on the real box** |
| `Get-PSDrive` | C: (393/82 GB), D: (672/260 GB) | sees the **whole disk**, any path — not a `/workspace` box |
| `Get-Command python,node,go,git,docker,wsl` | python 3.13, node 22.15, go, git 2.51, docker, wsl | uses the **real host toolchain** |
| `Get-Process \| sort CPU` | top procs incl. `claude` @1755s CPU | live **host process introspection** |
| `Get-NetTCPConnection -Listen` | ports incl. **2468** (the sandbox itself) | can **see the sandbox from outside it** |
| `python -c "...sha256/sum..."` | `sum 1..1e6 = 500000500000` | **arbitrary code, inline, instant** |
| `Get-Process` aggregate | 434 procs, 23.4 GB RAM | **composition / pipelines** over live state |
| `uname -a` via Bash | `Windows_NT ... x86_64` | **two shells** (PowerShell + POSIX), same host |

None crossed an HTTP boundary to a sidecar. In Aura today, *every one* would be a `sandbox_exec` →
`POST http://127.0.0.1:2468/v1/processes/run` → Docker spawn, and most (disk, ports, host processes,
host PATH) **could not run at all** — the locked-down container can't see them.

## Decomposition — Claude Code's "terminal power" is six primitives

| Primitive | Claude Code tool | What it subsumes |
|---|---|---|
| Run any command | `Bash` (keystone) | python/node/go, git, docker, ports, processes, pipelines, background jobs |
| Read a file | `Read` | line-numbered, offset/limit, images/PDF/notebooks |
| Write a file | `Write` | — |
| Exact-match edit | `Edit` | structured, unique-match replace |
| Content search | `Grep` | ripgrep, regex, globs, context |
| Filename search | `Glob` | fast path patterns |

`Read/Write/Edit/Grep/Glob` are *ergonomic specializations* of what `Bash` could already do — given as
first-class tools because they're hot paths that benefit from structure.

## Parity matrix — Aura today vs Claude Code

Ground truth: `cmd/aura/main.go:100-122` + `internal/agent/tools/`.

| Capability | Claude Code | Aura today | Verdict |
|---|---|---|---|
| **Full shell** | `Bash` (in-process, host) | `sandbox_exec` → HTTP → Docker `/workspace`, one cmd/call, token-gated | ❌ **boxed, indirected, locked down** |
| **Read file** | `Read` | none (must `sandbox_exec cat`) | ❌ missing |
| **Write file** | `Write` | none | ❌ missing |
| **Edit file** | `Edit` | none | ❌ missing |
| **Grep** | `Grep` | none (`bm25` is for tools, not fs) | ❌ missing |
| **Glob** | `Glob` | none | ❌ missing |
| Sub-agents / parallel | `Agent` / `Workflow` | `swarm_spawn` | ✅ have |
| Background tasks / schedule | `Cron*` / bg Bash | `task` + cron slice | ✅ have |
| Skills | `Skill` | `skill` | ✅ have |
| Deferred tool loading | `ToolSearch` | `tool_search` | ✅ have |
| HITL questions | `AskUserQuestion` | `ask_user` | ✅ have |
| Output paging | truncation + paging | `read_tool_output` | ✅ have |
| Web | `WebSearch` / `WebFetch` | `web_search` / `web_fetch` | ✅ have |
| Memory | flat files | Neo4j + PG graph (MCP) | ✅ have (richer) |

Aura is at parity or ahead on every *orchestration* row, behind on exactly the six *primitive* rows.
The gap is clean and contained — not a rewrite.

## Target toolset

### Tier 0 — native primitives (NEW; the missing hands)
- `shell_exec` — **full shell** in Aura's sandbox home; persistent session (Slice 2b), cwd + env
  persist, background jobs, `sh -c "..."` pipelines. The keystone.
- `fs_read` / `fs_write` / `fs_edit` / `fs_grep` / `fs_glob` — operate **in-process on the host side of
  the bind-mounted workspace** (instant, no HTTP). The container and the host see the same files; file
  ergonomics are native, command execution is in the sandbox home.

### Tier 1 — orchestration (HAVE; keep)
`swarm_spawn`, `task`, `skill`, `tool_search`, `ask_user`, `read_tool_output`, `current_time`,
`text_response`.

### Tier 2 — knowledge / world (HAVE; keep)
`web_search`, `web_fetch`, Neo4j + PG graph memory via `mcp-neo4j-cypher`.

## The sandbox home

- Aura's home = the **session-bound sandbox container** (Slice 2b) — a full, persistent terminal.
- **Bind-mount host dirs into it:** the workspace folder (`$AURA_RUN_DIR` / a workspace root) **and the
  skills folder** (`$AURA_SKILLS_DIR`). Both are present in the shell *and* directly reachable by Aura's
  native fs tools on the host side of the mount.
- `sandbox_exec` evolves into `shell_exec`: full shell, persistent session, no per-command lockdown.

## Security toys to drop (owner call: "no stupid security toys")

For a single-user personal assistant on the owner's own mini-PC, these add friction without protecting
against the actual threat model (the operator already trusts the box). They come off:

| Toy | Where it lives | Action |
|---|---|---|
| Bearer token on every exec/health call (D-38) | `internal/sandboxagent/client.go:77-83`, spike 008 | **drop** (run `--no-token`) |
| Egress allowlist proxy | spike 009 (`009-sandbox-egress-allowlist/proxy`) | **drop** |
| gVisor / runsc isolation | spike 010 | **drop** |
| Read-only skills mount | spike 005 | **invert** → skills folder mounted **read-write** in the home |
| `/workspace`-only fence, one-cmd-per-HTTP-call API | `sandbox_exec.go` + sandbox-agent | **replace** with full persistent shell + workspace **and** skills mounted |
| Pending "restore Phase-8 isolation hardening" TODO | `.planning/todos/pending/2026-06-05-restore-phase-8-sandbox-isolation-hardening.md` | **cancel** (superseded by this decision) |

## The one real (non-toy) boundary — kept for later

The single security control that is *not* a toy is **identity-scoped capability grants** (Slice 1.7).
It stays dormant now (single trusted operator = full power) and becomes the real gate the day Aura
faces Telegram / multiple users — that's where host/terminal power gets fenced per identity. This is
the one place "more isolation" will be earned by a real threat model, not added as theater.

## Lean change-set (ordered)

1. **Mount skills (rw) + workspace into the sandbox home**; make the session-bound sandbox (2b) the
   default exec environment. (`compose.yaml`, `docker/sandbox-agent/`, config.)
2. **`sandbox_exec` → `shell_exec`:** full shell (`sh -lc`), persistent session, drop token auth.
   (`internal/agent/tools/`, `internal/sandboxagent/`, `internal/config`.)
3. **Add native fs tools** (`fs_read/write/edit/grep/glob`) over the host side of the bind mount.
4. **Strip the toys:** token (D-38), egress proxy, gVisor, ro-skills-mount; cancel the hardening TODO.
5. **Register the new tools** in the composition root (`cmd/aura/main.go`).

## Next action (when greenlit)

This reverses shipped, committed decisions (D-38 token auth + spikes 008/009/010/005), so it lands as a
**short PRD amendment** so the codebase stays coherent — then one slice. Lean, no ceremony beyond that.
