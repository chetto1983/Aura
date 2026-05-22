# Fresh User Install Friction Log — 2026-05-22 (v0.3.0)

Simulated first-time install on Windows + Git Bash, following [`INSTALL.md`](../INSTALL.md) literally. Every friction point logged here is a real obstacle a new user hits.

## Setup

- Disk wiped: no `data/`, `runtime-workspace/`, `garage/`, Docker volumes — true zero-state
- Docker Desktop: running on Windows
- Shell: Git Bash (mirrors what a Mac/Linux/WSL user sees)
- Repo: fresh clone equivalent (post-`git clone` state)
- Image: tag `v0.3.0` published to `ghcr.io/chetto1983/aura` 5 min before this session
- Variables in INSTALL.md Step 2 followed verbatim (modulo PowerShell→Bash translation)

## Friction points

### F1 — INSTALL.md misleads about "GHCR-only install"

> **INSTALL.md line 7:** *"Current releases are Docker-image only. Use `ghcr.io/chetto1983/aura:<version>` with Docker Compose."*

**Reality:** only the `aura` service uses the GHCR image. Five sidecars require a **local Docker build** at first start:

| Service | Build source | Estimated cold time |
| --- | --- | --- |
| `aura-init-models` | `docker/init-models/Dockerfile` | ~30 s |
| `garage-init` | `docker/garage-init/Dockerfile` | ~30 s |
| `aura-markitdown` | local Python container | ~3 min |
| `aura-whisper` | whisper.cpp compiled from source | ~25 min (CPU build of native binaries + GGML) |
| `aura-pocket-tts` | Kyutai TTS.cpp compiled from source | ~15 min |

Cold first-start total: **~45-60 min**. INSTALL.md Step 3 ("Open the dashboard at `http://127.0.0.1:18080`") implies you can do it right after Step 2's command exits. You cannot.

**Severity: HIGH.** User stares at a 45+ min wait with no progress indicator and no expectation set.

**Fix proposals:**
- Add a "Cold start expectations" note to INSTALL.md Step 2: "First start builds 5 sidecars from source — expect 45-60 min."
- Add `docker compose logs -f aura-whisper aura-pocket-tts` watch command so user sees something happening.
- Consider publishing the 5 sidecars to GHCR too (would shrink Step 2 to a true pull).

---

### F2 — PowerShell-only command snippets

INSTALL.md Step 2:

```powershell
git clone https://github.com/chetto1983/Aura
cd Aura
New-Item -ItemType Directory -Force data,runtime-workspace,garage | Out-Null
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:latest"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

No Bash/zsh/sh alternative shown. A Mac/Linux/WSL user must translate:

```bash
mkdir -p data runtime-workspace garage
AURA_IMAGE="ghcr.io/chetto1983/aura:latest" docker compose -f compose.yaml -f compose.image.yaml up -d
```

The translation is mechanical but new users on non-Windows shells get tripped up.

**Severity: MEDIUM.** Project's primary distribution implicitly assumes Windows host.

**Fix proposal:** add a `bash`/`sh` snippet next to each PowerShell block. Three lines duplicated; worth it.

---

### F3 — Disk space prerequisites not documented

INSTALL.md "Prerequisites" lists Docker Desktop + Telegram bot token + LLM endpoint. Nothing about disk.

**Reality:**
- Docker build cache for the 5 sidecars: ~30-35 GB during build, ~15-20 GB after squash.
- Aura image: ~600 MB.
- Whisper model: ~487 MB (downloaded via init-models).
- Embed model: ~265 MB.
- Subsequent Docker images (qdrant + garage + searxng + llama.cpp + ...): ~3-5 GB combined.

**Total disk needed: ~25 GB free** for a healthy first start. INSTALL.md says nothing.

**Severity: MEDIUM.** First-time user on a small SSD will fail mid-build with "no space left on device".

**Fix proposal:** Prerequisites bullet "**~25 GB free disk** (Docker build cache + downloaded models)."

---

### F4 — Telegram bot creation order forced

INSTALL.md does Step 1 (create Telegram bot) BEFORE Step 2 (start Aura) BEFORE Step 3 (run setup wizard which asks for the token).

Issue: the user is told to leave Telegram, copy the token, then wait 45+ min for the build to finish, then come back and paste the token into a wizard. By the time the wizard opens, the user may have:

- Lost the BotFather chat / closed the Telegram window
- Forgotten the token (it's a long string)
- Forgotten the bot's username (needed in Step 4 to message it)

**Severity: LOW.** Real but cosmetic.

**Fix proposal:** Reorder so Step 1 is "Start Aura (this takes 45 min — see F1)" and Step 2 is "While waiting, create your Telegram bot."

---

### F5 — No mention of `compose.prod.yaml` vs `compose.image.yaml` distinction in INSTALL.md

README quick-start mentions `compose.prod.yaml` for production hardening. INSTALL.md only uses `compose.image.yaml`. The user reading INSTALL.md → README → docs gets three different compose-file recipes:

| Recipe | Source | Effect |
| --- | --- | --- |
| `docker compose up -d --build` | INSTALL.md "Development" section + README "Develop" | Local build of `aura` |
| `docker compose -f compose.yaml -f compose.image.yaml up -d` | INSTALL.md Step 2 + README Quick Start | Pull `aura` from GHCR, build sidecars locally |
| `docker compose -f compose.yaml -f compose.prod.yaml up -d` | README "Quick Start" production hardening block | Includes prod overlay |

Combining `compose.image.yaml` + `compose.prod.yaml` is never shown. Is the production recipe `up -d -f compose.yaml -f compose.image.yaml -f compose.prod.yaml`? Unclear.

**Severity: MEDIUM.** Confusion about which file combo is "the right one."

**Fix proposal:** explicit decision tree in INSTALL.md: "Choose your recipe:" → 3 options → recommend `compose.image.yaml` for end users, `compose.prod.yaml` overlay on top for hardened deployments.

---

### F6 — `runtime-workspace/` content expectations unclear

`runtime-workspace/` is created by Step 2 but it's EMPTY at first start. Aura at boot expects:

- `runtime-workspace/mcp.json` (MCP server config)
- `runtime-workspace/AGENT.md`, `SOUL.md`, `USER.md`, `TOOLS.md` (prompt overlays)

When these files are absent, Aura must copy defaults from `internal/config/defaults/` baked into the image. The user has no way to know what's there or how to customize.

**Severity: LOW** (Aura should handle this gracefully).

**Fix proposal:** verify Aura's first-boot logic actually copies defaults; if it does, document it in INSTALL.md Step 3: "Your overlays live in `runtime-workspace/*.md`. Edit them anytime; Aura reloads on save."

---

## Friction points to verify (pending install completion)

- F7: Does the setup wizard show up? Is the URL `http://127.0.0.1:18080` correct on fresh start, or is there a chicken-and-egg with the wizard listening only on loopback?
- F8: Does `aura-init-models` SHA-verify fail on download? Memory `feedback_codex_parallel_session_pattern` notes flaky downloads from HF.
- F9: Does pocket-tts download its own models at runtime (per recent README update) and is that visible to the user?
- F10: Does `garage-init` fail on first boot because the secret file isn't there yet?
- F11: Telegram bot connection: does the bot actually respond on `/start`?
- F12: First wiki ingest: drop a PDF, watch the OCR + ingest pipeline; observe whether it completes end-to-end without operator intervention.

---

*This file is the living friction log. Update each entry with verification status as the install completes.*
