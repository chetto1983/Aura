# Skills Self-Extension (discovery + install + use)

## Requirements

- **NO install-approval ceremony** (operator directive 2026-06-05): Aura self-installs
  with `npx skills add` via the sandbox terminal, Claude-Code-style. No ask_user gate,
  no pending/approve round-trip. Supersedes D-03/D-35 install-prudence. PRD amendment
  REQUIRED before the implementation lands.
- Discovery + install teaching = **skill content** (a find-skills adaptation riding the
  messages[1] always-block), NOT system-prompt routing or bespoke Go tools.
- The ONE hard security keep: the injection blocklist (`AURA_SKILL_INJECTION_BLOCKLIST`)
  must scan at **Loader level** — self-installed bodies never pass the Writer.
- Skill/snippet execution: always **by interpreter + path**, never the exec bit.
- 7a frontmatter parser = real YAML lib + CRLF normalization (double-quoted scalars
  with escaped quotes are in the wild — anthropics/skills/xlsx has them).

## How to Build It

The reference shape is production-proven twice: nanobot (`D:/tmp/nanobot` — 242-LOC
loader + ~55-line `skills/clawhub/SKILL.md`, its ENTIRE skills system) and spike 012a
(4/4 live PASS vs DeepSeek-V4; full autonomous loop in ONE turn, 212s).

1. **Ship `find-skills-aura` as a builtin skill** (`skills.MaterializeBuiltins`). The
   proven body is `sources/012a-discovery-skill-driven/find-skills-aura/SKILL.md`:
   `npx skills find <format-keyword>` → choose by installs+provenance →
   `npx skills add <owner/repo> --skill <name> --copy -y` → read installed SKILL.md →
   run bundled scripts by path. `always: true` → rides messages[1]
   (`RenderAlwaysBlock`, RoleUser, `"Active skill instructions (always-on):\n\n"` header).
2. **Delete the model-facing discovery/install complex** (~2,050 LOC with tests —
   013's table): `internal/skills/catalog.go`+test, `internal/agent/tools/
   skill_install.go`+test (both actions), `internal/skills/installer.go`+tests, the
   `cmd/aura/skills.go` catalog/install/approve legs, serve/eval adapters. Shrink the
   §Skills system-prompt section (amendment #49's 5-step REQUIRED workflow) to a
   pointer at the skill.
3. **Persistent installs** (the nanobot `--workdir` analog): in-sandbox
   `cd /skills && npx skills add …` writes through the rw bind to the host
   `AURA_SKILL_EXPORT_DIR` (amendment #50 made /skills rw). The CLI lands skills under
   `<cwd>/.agents/skills/<name>/` (its agent detection) — wire the Loader root to
   match (scan `<export>/.agents/skills` or teach a flat target in the skill body).
   Session-scoped installs (cwd=/workspace) need zero wiring and already work.
4. **Move the blocklist scan into `skills.Loader`** so any body entering the manifest/
   always-block — authored OR self-installed — is scanned at load time.
5. **Eval**: rewrite the D-35 scenario gate to artifact + self-install evidence
   (the 012a harness pattern: action-aware capture from STRUCTURED tool args +
   `docker exec find /workspace -name '*.xlsx' -newermt '<run-start>'`). The shipped
   capture records only tool NAMES — it cannot see skill actions.

CLI contract (spike 011, live-probed):
- Piped/no-TTY `npx skills find <q>` prints `owner[/repo]@skill N[K|M] installs` +
  skills.sh URL per result, ranked by installs; exit 0; empty query prints agent-
  targeted usage (no hang). ANSI always on (NO_COLOR ignored) — strip
  `\x1b\[[0-9;?]*[a-zA-Z]`.
- `npx skills add <owner/repo> --list` = clone + enumerate a multi-skill repo WITH
  full descriptions (no install). The model used it unprompted in live runs.
- Warm ~3s, cold ~4s (npx fetch ≈ +1s); in-sandbox 1.6-2.3s (node 22 baked).
  vs spike-003 Go HTTP client 0.25-0.7s — irrelevant inside an LLM turn.
- Single-token format queries ("xlsx") rank dramatically better than topic phrases;
  the find-skills body teaches exactly this.

## What to Avoid

- **Do NOT rebuild the tool-driven flow.** The shipped `action=catalog`/`install`
  needed 6 amendment-#49 prompt iterations and STILL failed its E2E (judge 0.47).
  Root cause (012b): the pause machinery is name-gated to `ask_user`
  (`llm_agent_pause.go`) — a non-ask_user tool returning `ErrAwaitingUserInput`
  collapses to the literal `"error: awaiting user input"` (`runTool`,
  `ask_user.go:83`), so `renderInstallGate`'s red flags/question were DEAD CODE. If
  any future tool needs to pause the turn, it must route through ask_user or the
  dispatch must learn to surface tool sentinels — never assume a tool error pauses.
- **Don't chase skills-lock.json `computedHash` interop** (004a/004b): upstream's
  localeCompare sort makes it platform/locale-sensitive — even the CLI's own `check`
  mismatches a Windows checkout. If pinning is ever needed again, use Aura's canonical
  hash (byte-sorted (relPath, bytes) sha256 — implementation in
  `sources/004b-install-native-clone/main.go`).
- **Don't trust marketplace ranking or name uniqueness**: ranking re-ordered within
  hours between spikes 003 and 011; a 76K-install name-squat of find-skills outranks
  the 1.9M-install original on semantic queries. Provenance teaching in the skill body
  (installs + reputable sources) handled this in all live runs.
- **Don't ship a binary with "install" in its filename on Windows** (UAC Installer
  Detection demands elevation — 004b).
- skills.sh `/api/search` is internal/undocumented (vercel-labs/skills#426) with
  drifting fields — a reason the official CLI beats a bespoke client. If a Go client
  is ever needed again: lax decode only, guard empty queries (400), contract in
  `sources/003-skills-sh-search-api/`.

## Constraints

- npx requires node host-side OR in-sandbox — the sandbox image already ships node
  v22 + npx + git (zero image change).
- npm-registry + github.com + skills.sh egress needed for discovery/install — joins
  the native-Linux allowlist conversation (see sandbox-runtime reference).
- Superseded-but-preserved: 004b proved native git-clone install (1.49s, bit-identical,
  3× faster than npx) — moot while the model drives the CLI itself; revives only if a
  Go-side install path returns.

## Origin

Synthesized from spikes: 003, 004a, 004b, 011, 012a, 012b, 013
Source files: sources/003-skills-sh-search-api/, sources/004a-install-npx-cli/,
sources/004b-install-native-clone/, sources/011-npx-find-noninteractive/,
sources/012a-discovery-skill-driven/, sources/012b-discovery-tool-driven/,
sources/013-thin-surface-gate-parity/
