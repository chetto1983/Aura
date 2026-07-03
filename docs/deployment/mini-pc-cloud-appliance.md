# Mini-PC Cloud Appliance — Aura on 8 GB, all models via OpenRouter

Deployment guide for running Aura as a **low-power, always-on appliance** on a
small mini PC (target: **8 GB RAM, no GPU**) with **all inference in the cloud**
(OpenRouter). The heavy local-inference stack stays on the workstation; the mini
PC only runs the orchestration + data plane and calls OpenRouter for chat, vision,
and (optionally) embeddings.

> Companion: the full **local-inference** stack (GPU sidecars, local Qwen3-8B) is
> for the 32 GB + RTX workstation — see the base `compose.yaml` / `compose.gpu.yaml`
> / `compose.llm.yaml` and the project memory `aura-wsl-infra-setup`.

---

## 1. Topology

```
┌───────────────────────────┐        ┌────────────────────────────────┐
│  Mini PC — 8 GB, no GPU    │        │  Workstation — 32 GB + RTX 3060 │
│  ALWAYS-ON APPLIANCE       │        │  LOCAL-INFERENCE / DEV          │
│                            │        │                                 │
│  Postgres · Neo4j(tuned)   │        │  full stack + GPU sidecars:     │
│  aura · agent-memory-mcp   │        │  embed · rerank · ocr · stt ·   │
│  garage · whatsapp · pim   │        │  tts · markitdown · local       │
│                            │        │  Qwen3-8B (compose.llm.yaml)    │
│   chat/vision/​embed ──────┼──▶ OpenRouter ◀──────┼── (optional cloud) │
└───────────────────────────┘        └────────────────────────────────┘
```

"All cloud" removes the **GPU** load, not the whole footprint — Postgres, Neo4j,
the `aura` daemon and its sibling MCP sidecars still run locally.

---

## 2. Host OS

The host only needs to run `docker compose` reliably; the containers bring their
own userland, so host libc/init is invisible to them. What matters: **systemd**
(Aura's `deploy/aura.service` appliance unit is a systemd unit), **glibc + apt**
(Aura's docs/tooling assume Debian/Ubuntu), and low idle RAM.

| Distro | Verdict |
|---|---|
| **DietPi** ⭐ | Recommended. Debian-based (systemd/glibc/apt = full compatibility), ~200–400 MB idle, `dietpi-software` installs Docker + compose in one step. Purpose-built for a low-RAM appliance. |
| **Debian 13 minimal** (netinst, no DE) | Equally safe, boring, bulletproof. ~400–500 MB idle. Pick if you prefer maximum "just works" over shaving RAM. |
| Ubuntu Server | Only if you want it identical to the workstation; heavier (snapd), no benefit here. |
| ❌ Alpine / Tiny Core | Great container *bases*, but musl + OpenRC (no systemd) fights the appliance unit + host tooling for a ~270 MB gain — not worth it on 8 GB. |

The ~270 MB idle difference between "tiniest" and "minimal Debian" is noise next to
the ~5 GB workload. **Reliability + compatibility win — use DietPi or Debian minimal.**

---

## 3. Base install

```bash
# 1) Install DietPi (or Debian 13 minimal, headless). Then, as root/sudo:

# 2) Docker Engine + compose plugin
#    DietPi:  dietpi-software  → install "Docker" + "Docker Compose"
#    Debian:  curl -fsSL https://get.docker.com | sh
docker --version && docker compose version

# 3) Tooling to run host-side migrations (Go) — only if NOT running migrations
#    inside the aura-migrate container (the appliance runs them automatically).
sudo apt-get update && sudo apt-get install -y git curl ca-certificates

# 4) Clone the repo onto NATIVE ext4 (never a slow mount)
sudo mkdir -p /opt && cd /opt
git clone <your-aura-remote> aura && cd /opt/aura
```

> Migrations: the `aura-migrate` compose service runs `db migrate && neo4j migrate`
> automatically at stack start, so you do **not** need Go on the mini PC unless you
> want to run migrations by hand.

---

## 4. Configuration

### 4a. `.env` — generate secrets + set the cloud knobs

Generate the base `.env` exactly as on the workstation (all 9 required secrets):

```bash
cp .env.example .env
gen() { openssl rand -hex "$1"; }
fill() { sed -i "s|^$1=\$|$1=$2|" .env; }
fill POSTGRES_PASSWORD "$(gen 32)";            fill NEO4J_PASSWORD "$(gen 32)"
fill AURA_ACCESS_TOKEN "$(gen 32)";            fill AURA_AUTHULA_SECRET "$(gen 32)"
fill AURA_PIM_MCP_ADMIN_TOKEN "$(gen 32)";     fill SEARXNG_SECRET "$(gen 32)"
fill AURA_OBJECTSTORE_ACCESS_KEY "GK$(gen 12)"; fill AURA_OBJECTSTORE_SECRET_KEY "$(gen 32)"
fill GARAGE_RPC_SECRET "$(gen 32)"
chmod 600 .env
```

Then set the cloud-appliance knobs (append / edit in `.env`):

```dotenv
# ---- REQUIRED: the single cloud key every backend reuses ----
OPENROUTER_API_KEY=sk-or-...

# ---- Chat LLM → OpenRouter (already the default base) ----
AURA_LLM_BASE_URL=https://openrouter.ai/api/v1
AURA_LLM_MODEL=deepseek/deepseek-v4-flash:nitro      # or your preferred OpenRouter chat model

# ---- Vision → cloud (no local aura-ocr-vl needed) ----
AURA_VISION_CLOUD=true
MULTIMODAL_FALLBACK_MODEL=minimax/minimax-m3         # cloud VL fallback

# ---- Embeddings: keep 768 to match the Neo4j HNSW index (Amendment #18) ----
AURA_EMBED_DIMENSIONS=768
```

Then choose ONE embedding strategy:

**Option A — local granite on CPU (recommended for a GPU-less box).**
Free, no per-chunk API latency, natively 768-dim (no truncation risk). The 311M
model is fine for personal RAG. Just swap the embed sidecar to the CPU image:

```dotenv
AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server   # CPU build (NOT :server-cuda)
AURA_EMBED_NGL=0                                      # no GPU offload
# leave AURA_EMBED_MODEL UNSET → EmbedRoute stays local (config_routes.go:38)
```

**Option B — embeddings via OpenRouter (`qwen/qwen3-embedding-4b`).**
Zero local inference, but every chunk is a paid API call. Verified compatible:
`EmbedRoute()` swaps to OpenRouter when `AURA_EMBED_MODEL` is set, and the embedder
sends `dimensions:768` on the cloud route (`internal/documents/embedder.go:44-49`),
which Qwen3-Embedding-4B (native 2560, MRL 32–2560) truncates to 768.

```dotenv
AURA_EMBED_MODEL=qwen/qwen3-embedding-4b             # → cloud embed via OPENROUTER_API_KEY
# Also point the memory-MCP sidecar's embedder at the cloud (D-28):
AURA_MEMORY_EMBED_BASE_URL=https://openrouter.ai/api/v1
AURA_MEMORY_EMBED_API_KEY=${OPENROUTER_API_KEY}
AURA_EMBED_MODEL=qwen/qwen3-embedding-4b
# NOTE: even with cloud embed, the aura/​memory-mcp containers still `depends_on`
# aura-llama-embed (compose.yaml). Keep Option A's CPU image vars set so the sidecar
# starts healthy and satisfies the dependency (it just goes unused). ⚠️ You MUST run
# the §6 curl check: if OpenRouter returns 2560 (param not honored) Aura fails LOUD
# ("embedding N has dimension 2560, want 768") — clean fail, never corruption.
```

> **Recommendation:** Option A. On a GPU-less always-on box, local CPU granite is
> simpler, free, has no dimension risk, and the sidecar is actually used. Reach for
> Option B only if you want literally zero local inference.

### 4b. `compose.cloud.yaml` — tune Neo4j down (heap is the RAM hog)

The Neo4j memory settings are hardcoded in `compose.yaml`, so a tiny override shrinks
them for 8 GB. (Everything else — embed image/NGL, vision — is `.env`-driven above.)

```yaml
# compose.cloud.yaml — mini-PC memory tuning. Layer LAST:
#   docker compose -f compose.yaml -f compose.cloud.yaml up -d aura caddy
services:
  neo4j:
    environment:
      NEO4J_server_memory_heap_initial__size: 256m
      NEO4J_server_memory_heap_max__size: 512m
      NEO4J_server_memory_pagecache_size: 256m
```

This file is created alongside this guide. **Do NOT** layer `compose.gpu.yaml` or
`compose.llm.yaml` here — those are workstation-only.

---

## 5. Bring-up

The `aura` appliance container has a fixed dependency set it auto-starts: `postgres`,
`neo4j`, `aura-llama-embed` (CPU per §4a), `aura-agent-memory-mcp`, `garage`(+bootstrap),
`whatsapp`, `aura-pim-mcp`, and the one-shot `aura-migrate` (runs both migrations).
The GPU/media sidecars (`rerank`, `ocr-vl`, `stt`, `tts`, `markitdown`) are **not**
dependencies, so they never start unless you ask for them.

```bash
cd /opt/aura
docker compose -f compose.yaml -f compose.cloud.yaml up -d aura caddy
# migrations run automatically inside aura-migrate; watch it:
docker compose logs -f aura-migrate
docker compose ps
```

---

## 6. Acceptance checks

```bash
# All expected services healthy
docker compose ps --format '{{.Name}}\t{{.Status}}'

# Embedding dimension MUST be 768 (whichever embed option you chose)
#  - Option A (local CPU granite): granite is natively 768.
#  - Option B (OpenRouter): confirm the provider honors dimensions=768 —
curl -s https://openrouter.ai/api/v1/embeddings \
  -H "Authorization: Bearer $OPENROUTER_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"qwen/qwen3-embedding-4b","input":"ciao","dimensions":768}' \
  | jq '.data[0].embedding | length'          # MUST print 768 (not 2560)

# Setup wizard / cockpit
#   https://<mini-pc-ip>/setup/?token=$(grep ^AURA_ACCESS_TOKEN .env | cut -d= -f2)
```

If the curl prints `2560`, OpenRouter is not passing `dimensions` for that model —
switch to embed Option A (local CPU) or a model/provider that honors it. Aura fails
loudly either way, so the index is never silently corrupted.

---

## 7. Footprint (8 GB budget)

| Component | ~RAM | Notes |
|---|---|---|
| Neo4j (tuned 512m heap / 256m pagecache) | ~1.2 GB | the biggest single consumer |
| Postgres | ~0.3 GB | |
| aura daemon | ~0.6 GB | `mem_limit 768m` |
| agent-memory-mcp | ~0.4 GB | Python |
| garage + whatsapp + pim | ~0.5 GB | aura hard-deps; pim is .NET |
| embed (CPU granite, Option A) | ~0.5 GB | omit if Option B, but sidecar still runs |
| Docker + OS (DietPi) | ~1.5 GB | |
| **Total** | **~5.0–5.5 GB** | fits 8 GB with headroom |

> Leaner still: run `aura serve` as a **host binary** (Go) against just Postgres +
> Neo4j + cloud, skipping the garage/whatsapp/pim/memory-mcp sidecars — only do this
> if you don't need object storage, WhatsApp, calendar, or graph memory.

---

## 8. Cost & latency notes

- Every chat turn **and** (Option B) every embedded chunk is a metered OpenRouter
  call — trade free/instant local inference for API cost + network latency.
  `qwen/qwen3-embedding-4b` is ~$0.02/M tokens (cheap); chat cost depends on the model.
- Option A keeps embeddings free/local (only chat + vision are metered) — usually the
  better economics for an always-on personal assistant.
- The appliance is loopback-published behind Caddy; expose it on your LAN/VPN, not the
  public internet, unless you've hardened auth (Authula) — see `compose.yaml` WEB-02.

---

## 9. See also

- Base runtime: [`compose.yaml`](../../compose.yaml) · GPU: [`compose.gpu.yaml`](../../compose.gpu.yaml) · local LLM: [`compose.llm.yaml`](../../compose.llm.yaml)
- Route resolver (one-knob local↔cloud): [`internal/config/config_routes.go`](../../internal/config/config_routes.go)
- Embed client (sends `dimensions`, validates width): [`internal/documents/embedder.go`](../../internal/documents/embedder.go)
- Appliance installer: [`scripts/install.sh`](../../scripts/install.sh) (`--appliance` for the systemd unit)
