---
spike: 005
name: skills-ro-mount
type: standard
validates: "Given sandbox-agent compose + a host export dir mounted ro at /skills, when a .py is materialized host-side and exec'd by path via the sandbox-agent run API, then it runs in-container, host edits are live-visible, and in-container writes to /skills are refused"
verdict: VALIDATED
related: []
tags: [skills, sandbox, mount, docker-desktop, phase-11]
---

# Spike 005: skills-ro-mount

## What This Validates

Phase-11 D-17's physical foundation: active skill snippets materialize as files in a host export dir bind-mounted **read-only** at `/skills` inside `aura-sandbox-agent`, and the model executes them **by path** via `sandbox_exec` (the D-04 Claude-Code-parity design). This is the stack's FIRST bind mount — today's compose uses named volumes only ("named volumes mandatory, no bind-mount Windows" was a standing project constraint born from the `docker volume create --opt device=` 0x100e failure class on Docker Desktop).

## Research

No external docs needed — the question is purely local ground truth. Key distinction vs the historical failure: a compose **bind mount** (`D:/path:/skills:ro`) rides Docker Desktop's file-sharing layer (gRPC-FUSE), not the daemon-side `device=` volume-driver path resolution that failed in Phase 8 (CAP-02 workspace criteria). Different mechanism, needed proving.

## How to Run

```bash
# 1. Apply the override (recreates aura-sandbox-agent with the ro bind):
docker compose -f compose.yaml -f .planning/spikes/005-skills-ro-mount/compose.skills-mount.yaml up -d aura-sandbox-agent
# 2. Harness:
go run ./.planning/spikes/005-skills-ro-mount
# 3. Restore (drop the mount):
docker compose -f compose.yaml up -d aura-sandbox-agent
```

## What to Expect

Forensic `[CATEGORY]` log lines; 4 PASS lines (by-path exec, live visibility, ro enforcement, de-materialization); `[SUMMARY] VALIDATED`; exit 0.

## Investigation Trail

1. **Happy path (harness, 4/4 PASS first run):** host-materialized `hello.py` executed by path (`python3 /skills/hello.py`) → marker read back; host rewrite visible in-container **immediately** (no FUSE cache lag — the 15s retry loop never looped); `touch /skills/evil` → `Read-only file system`, exit 1; host delete → exec fails Errno 2 (archive/delete lockstep proven).
2. **Nested dir created AFTER container start:** `export/xlsx/scripts/run.sh` (the installed-skill layout) — immediately visible, `sh /skills/xlsx/scripts/run.sh` exit 0 (`NESTED-SH-OK 42`). New-directory propagation works, not just file edits.
3. **Exec bit:** the share presents everything `-rwxrwxrwx` (Docker Desktop perm-masking) — direct exec would work HERE but won't on native-Linux prod unless the writer chmods. **Planner rule: always invoke via interpreter (`python3`/`sh` + path), never rely on the exec bit — portable across dev/prod.**
4. **Symlink probes:** Windows-side symlink creation needs admin (both MSYS nativestrict and PowerShell failed unprivileged) — but **WSL on /mnt/d creates them fine**, so skill content CAN carry symlinks on this host. In-container: an absolute symlink to a host path (`/mnt/d/...`) **dangles** (target absent in-container); an absolute symlink to a container-existing path (`/etc/passwd`) **RESOLVES** — `cat /skills/leak4.link` read the container's passwd.

## Results

**VALIDATED** — the ro bind mount works end-to-end on the Docker Desktop dev stack: by-path execution, immediate live visibility (both file edits and new directory trees), read-only enforcement from inside, clean de-materialization. The Phase-8 `device=` failure class does NOT apply to compose bind mounts.

**Surfaced findings for the planner:**
- **Symlinks in skill content resolve in-container.** New-capability risk ≈ none (the model can already read any container path via `sandbox_exec`), but HOST-side risk is real: installed-skill content carrying symlinks could trick host-side loader/writer walks. Mitigation: installer/writer **rejects/strips symlinks at materialization** (reuse the Phase-4 `ScanOrphans` Lstat-no-follow pattern). Make it an acceptance item of 7d/7e.
- **Always invoke by interpreter, never exec bit** (finding 3).
- The override file pins the exact mount shape tested (absolute Windows path, forward slashes, `:ro`). Production compose should use env interpolation (`${AURA_SKILL_EXPORT_DIR}`) — same mechanism, parametrized source.
- Live-visibility is immediate on this stack; do NOT design a sync/refresh step.
