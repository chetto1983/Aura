# Mini-PC Appliance — Aura on 8 GB, big models via OpenRouter

Deployment guide for running Aura as a **low-power, always-on appliance** on a
small mini PC (target: **8 GB RAM, no GPU**), with the heavy models in the cloud
(OpenRouter) and the data plane local.

The delta from the workstation stack is one committed file, `compose.minipc.yaml`.
Read it — it is short, and it is the authority. This page is the procedure around it.

> Companion: the full local-inference stack (GPU sidecars) is the base
> [`compose.yaml`](../../compose.yaml) on a 32 GB + RTX workstation.

---

## 1. Topology

```
┌────────────────────────────────┐        ┌────────────────────────────────┐
│  Mini PC — 8 GB, no GPU        │        │  Workstation — 32 GB + RTX     │
│  ALWAYS-ON APPLIANCE           │        │  LOCAL-INFERENCE / DEV         │
│                                │        │                                │
│  postgres · arcadedb           │        │  the same stack, plus the GPU  │
│  arcadedb-mcp · aura           │        │  sidecars: rerank · ocr-vl ·   │
│  aura-llama-embed (CPU)        │        │  stt · tts                     │
│  garage · whatsapp · pim       │        │                                │
│  searxng · markitdown · caddy  │        │                                │
│                                │        │                                │
│   chat / vision / stt / tts ───┼──▶ OpenRouter ◀────────┼──  (optional)   │
└────────────────────────────────┘        └────────────────────────────────┘
```

Going cloud removes the **GPU** load, not the footprint: Postgres, ArcadeDB, the
`aura` daemon and its sibling MCP sidecars still run locally.

**Embeddings stay local even here**, and that is deliberate. The adaptive-reasoning
classifier embeds 27 anchors before *every* turn, one call at a time: measured
21–44 ms locally against 3.9–43.3 s per call via OpenRouter. The cloud is for the
big models, not for this one.

---

## 2. Host OS

The host only has to run `docker compose` reliably; the containers bring their own
userland. What matters: **systemd** (the appliance unit is a systemd unit),
**glibc + apt** (the tooling assumes Debian/Ubuntu), and low idle RAM.

| Distro | Verdict |
|---|---|
| **DietPi** | Recommended. Debian-based, ~200–400 MB idle, `dietpi-software` installs Docker + compose in one step. |
| **Debian minimal** (netinst, no DE) | Equally safe and boring. ~400–500 MB idle. |
| Ubuntu Server | Only to match the workstation exactly; heavier (snapd), no benefit here. |
| Alpine / Tiny Core | Great container *bases*, but musl + OpenRC (no systemd) fights the appliance unit for a ~270 MB gain — not worth it on 8 GB. |

That ~270 MB is noise next to the ~5 GB workload. Reliability wins.

---

## 3. Base install

```bash
# 1) DietPi or Debian minimal, headless. Then as root/sudo:

# 2) Docker Engine + compose plugin
#    DietPi:  dietpi-software  → "Docker" + "Docker Compose"
#    Debian:  curl -fsSL https://get.docker.com | sh
docker --version && docker compose version

# 3) Clone onto NATIVE ext4 (never a slow mount)
sudo mkdir -p /opt && cd /opt
git clone <your-aura-remote> aura && cd /opt/aura
```

> Migrations run themselves: the one-shot `aura-migrate` service applies the
> Postgres sequence at stack start, and `aura` gates its own boot on it completing.
> ArcadeDB has no migration step — its schema is idempotent DDL applied at connect.
> You do not need Go on the mini PC.

---

## 4. Configuration

### 4a. Select the overlay — once, in `.env`

```dotenv
COMPOSE_FILE=compose.yaml:compose.minipc.yaml
```

From then on `docker compose up -d` is enough, with no `-f` to remember. That is
the whole point of the variable: an overlay you must pass by hand fails *silently*
the day you forget it — nothing errors, it just quietly changes where embeddings go
and whether the GPU is used.

### 4b. Secrets

Easiest path: `scripts/install.sh` generates every one of them and never overwrites
a value you already set. By hand:

```bash
cp .env.example .env
gen() { openssl rand -hex "$1"; }
fill() { sed -i "s|^$1=\$|$1=$2|" .env; }

fill POSTGRES_PASSWORD "$(gen 32)"
fill ARCADEDB_PASSWORD "$(gen 32)"
fill ARCADEDB_APP_PASSWORD "$(gen 32)"
fill AURA_ARCADEDB_TENANT_SECRET "$(gen 32)"
fill AURA_ACCESS_TOKEN "$(gen 32)"
fill AURA_AUTHULA_SECRET "$(gen 32)"
fill AURA_PIM_MCP_ADMIN_TOKEN "$(gen 32)"
fill SEARXNG_SECRET "$(gen 32)"
fill AURA_OBJECTSTORE_ACCESS_KEY "GK$(gen 12)"
fill AURA_OBJECTSTORE_SECRET_KEY "$(gen 32)"
fill GARAGE_RPC_SECRET "$(gen 32)"
fill AURA_GARAGE_ADMIN_TOKEN "$(gen 32)"
chmod 600 .env
```

All twelve use compose's `:?` required form, and compose interpolates the **whole
file** before it picks a service — so one missing name aborts every `docker compose`
invocation, including ones that touch none of those containers.
`docker compose config >/dev/null` is the check.

`AURA_ARCADEDB_TENANT_SECRET` is not an ordinary secret: per-identity ArcadeDB
credentials are *derived* from it (HMAC-SHA256 over the database name), so nothing
per-tenant is stored and rotating it invalidates every one of them at once. Plan
that; do not restart into it.

### 4c. Cloud knobs

```dotenv
# The single cloud key every backend reuses
OPENROUTER_API_KEY=sk-or-...

# Chat
AURA_LLM_BASE_URL=https://openrouter.ai/api/v1
AURA_LLM_MODEL=deepseek/deepseek-v4-flash:nitro

# The CPU embedding pair. Image and NGL are ONE choice, not two: disagree and the
# sidecar either fails to start or silently runs on CPU while you believe otherwise.
AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server
AURA_EMBED_NGL=0
AURA_EMBED_DIMENSIONS=768
```

Vision, STT, TTS and rerank are already pointed at OpenRouter by
`compose.minipc.yaml`, with their four GPU containers **deleted** rather than
stopped — a declared-but-never-started service is a trap, because a plain
`docker compose up -d` pulls it, and here that means a 5 GB CUDA image over WiFi
for a container that will not run.

### 4d. The embedding model file

The sidecar is started with a **local path**, not `--hf-repo`: it has no egress, so
a first boot that has to fetch from HuggingFace restart-loops. Pre-fetch it into the
cache volume once:

```bash
docker volume create aura_aura-llama-embed
docker run --rm -v aura_aura-llama-embed:/cache alpine sh -c \
  'apk add --no-cache curl >/dev/null && curl -fsSL -o /cache/embeddinggemma-300M-Q8_0.gguf <gguf-url>'
```

`AURA_EMBED_MODEL_PATH` defaults to that filename under
`/root/.cache/llama.cpp/`. The width is **768** — EmbeddingGemma-300M's native size,
and also the width of memory's dense HNSW index, which refuses a query vector of any
other length. That refusal is the failure you want; a silent mismatch is not.

### 4e. ArcadeDB heap

`ARCADEDB_OPTS_MEMORY` (default `-Xms2G -Xmx2G`) is a plain `.env` variable, so
there is no override file to write. On an 8 GB box:

```dotenv
ARCADEDB_OPTS_MEMORY=-Xms512m -Xmx1G
```

---

## 5. Bring-up

```bash
cd /opt/aura
docker compose up -d
docker compose logs -f aura-migrate    # the one-shot Postgres migration
docker compose ps
```

`aura` gates its boot on `postgres`, `aura-llama-embed` and `arcadedb-mcp` being
**healthy**, and on `aura-migrate` and `garage-bootstrap` having completed. The
memory sidecar is a hard gate on purpose: memory is mounted into the agent loop at
startup and the in-process mount has no boot retry, so a sidecar that is merely
*still starting* yields an agent with zero memory tools until the next restart.

`whatsapp` and `aura-pim-mcp` start with the stack but are `service_started`, never
`service_healthy` — boot must not block on them, and their connect routes fail soft
to 503.

---

## 6. Acceptance checks

```bash
docker compose config >/dev/null && echo "compose: parses"
docker compose ps --format '{{.Name}}\t{{.Status}}'

# Embedding width — MUST print 768
curl -s http://127.0.0.1:8081/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"input":"ciao"}' | jq '.data[0].embedding | length'

# Memory sidecar liveness
curl -sf http://127.0.0.1:8096/health && echo " memory: up"

# Full dependency probe
docker compose exec aura aura doctor
```

`aura doctor` reports four checks: Postgres, the embedding sidecar (printing the
dimension it got back), the presence of `OPENROUTER_API_KEY`, and a live probe of
every enabled runnable MCP server.

The setup wizard is at `https://<mini-pc-ip>/setup/` with the `AURA_ACCESS_TOKEN`
value as its token.

---

## 7. Footprint (8 GB budget)

| Component | ~RAM | Notes |
|---|---|---|
| ArcadeDB (`-Xmx1G` per §4e) | ~1.2 GB | the biggest single consumer; `mem_limit 3g` in compose |
| Postgres | ~0.3 GB | |
| aura daemon | ~0.6 GB | `AURA_MEM_LIMIT`, default 768m |
| arcadedb-mcp | ~0.2 GB | `mem_limit 512m`, Go |
| embed sidecar (CPU EmbeddingGemma-300M) | ~0.6 GB | |
| garage + whatsapp + pim + searxng + markitdown | ~0.8 GB | pim is .NET |
| Docker + OS (DietPi) | ~1.5 GB | |
| **Total** | **~5.2 GB** | fits 8 GB with headroom |

---

## 8. Cost & latency notes

- Every chat turn is a metered OpenRouter call. Embeddings are not — they are local,
  and §1 explains why that is not negotiable on the hot path.
- The appliance is loopback-published behind Caddy. Expose it on your LAN or VPN,
  not the public internet, unless Authula auth is configured (see `compose.yaml`
  WEB-02).

---

## 9. See also

- Base runtime: [`compose.yaml`](../../compose.yaml) · appliance delta: [`compose.minipc.yaml`](../../compose.minipc.yaml)
- Route resolver (one-knob local↔cloud): [`internal/config/config_routes.go`](../../internal/config/config_routes.go)
- Appliance installer: [`scripts/install.sh`](../../scripts/install.sh) (`--appliance` for the systemd unit)
