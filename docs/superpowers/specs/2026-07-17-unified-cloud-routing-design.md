# Unified cloud routing — mini-PC appliance (config now, Go refactor later)

**Date:** 2026-07-17
**Status:** Part 1 (config override) shipped & deployed; Part 2 (Go refactor) proposed, not started.
**Constraint:** deploy on the existing `aura:local` image — **no rebuild**.

## Context

Deploying Aura as an always-on appliance on a GEEKOM Mini Air12 (Intel N95, 4c,
16 GB RAM, **no GPU**, Ubuntu 26.04, `192.168.1.225`, `aura.local` via mDNS). Every
inference backend runs in the cloud (OpenRouter); the box runs only the
orchestration + data plane and its sibling MCP sidecars.

The user asked to route embed/rerank/STT/TTS/vision to specific cloud models and
"unify" the routing, which surfaced a real inconsistency in how the six modalities
resolve their local↔cloud swap.

## Problem — three swap mechanisms, incomplete plumbing

| Modality | Cloud switch | Base-URL convention | Swap mechanism |
|---|---|---|---|
| Chat | `AURA_LLM_MODEL` | `…/api/v1` (with /v1) | always cloud |
| Vision | `AURA_VISION_CLOUD` (bool) | `LLM.BaseURL` (with /v1) | bool branch |
| Embed | `AURA_EMBED_MODEL` | `…/api` (**no** /v1; client appends `/v1/embeddings`) | `isLoopbackURL(base)` swap |
| Rerank | `AURA_RERANK_MODEL` | `…/api` (**no** /v1; client appends `/v1/rerank`) | `isLoopbackURL(base)` swap |
| STT | `AURA_STT_CLOUD_MODEL` | `LLM.BaseURL` (with /v1) | `CloudModel != ""` branch |
| TTS | `AURA_TTS_MODEL` | `LLM.BaseURL` (with /v1) | `CloudModel != ""` branch |

Two concrete plumbing bugs in the container path:

1. **Base compose does not forward** `AURA_EMBED_MODEL`, `AURA_RERANK_MODEL`,
   `AURA_STT_CLOUD_MODEL`, `AURA_TTS_MODEL` to the `aura` service — setting them in
   `.env` is a no-op for the containerized daemon.
2. `AURA_EMBED_BASE_URL` / `AURA_RERANK_BASE_URL` are **hardcoded to the sidecar DNS**
   (`http://aura-llama-embed:8081`, `http://aura-rerank:8085`), which is **not
   loopback** — so the `isLoopbackURL` swap in `EmbedRoute`/`RerankRoute` never fires
   in the container, and a set cloud model would POST to the local sidecar with a
   Bearer key instead of OpenRouter. (The runbook's Option B was effectively broken
   for the container path for this reason.)

## Part 1 — config-layer unification (SHIPPED)

One override, `compose.cloud.yaml`, made **the** cloud-appliance file: it plumbs all
six modalities to OpenRouter on the running image, removes the GPU-only inference
sidecars from the dependency graph, and shrinks Neo4j. Bring-up:

```bash
docker compose -f compose.yaml -f compose.cloud.yaml up -d aura caddy
```

Cloud model map (all via the single `OPENROUTER_API_KEY`), **verified live** before
deploy:

| Modality | Model | Live check |
|---|---|---|
| Embed | `qwen/qwen3-embedding-8b` | `/v1/embeddings` returns **exactly 768** dims with `dimensions:768` |
| Rerank | `cohere/rerank-4-pro` | `/v1/rerank` HTTP 200, correct scoring |
| STT | `openai/whisper-large-v3-turbo` | `/audio/transcriptions` endpoint present |
| TTS | `hexgrad/kokoro-82m` | `/audio/speech` returns valid MP3 — **opus rejected, needs `TTS_FORMAT=mp3`** |
| Vision | `minimax/minimax-m3` | `AURA_VISION_CLOUD=true` |
| Chat | `deepseek/deepseek-v4-flash:nitro` | default |

Base-URL fix (verified against client code): embed/rerank bases set **without** `/v1`
(`https://openrouter.ai/api`) because those clients append `/v1/embeddings|/v1/rerank`;
STT/TTS use `LLM.BaseURL` **with** `/v1` automatically because they append `/audio/…`.

**Sidecar surgery:** the CUDA embed sidecar (`aura-llama-embed`) cannot start on a
GPU-less host and is a hard `service_healthy` dep of `aura` and the memory MCP. The
override `!reset null`s that dep on both, and re-declares the memory MCP boot command
with the `aura-llama-embed:8081` wait target removed (else 240 s timeout → crash-loop).
`stt`/`tts`/`rerank`/`ocr` are not dependencies, so they simply never start.

Running stack: `aura`, `aura-migrate`, `postgres`, `neo4j` (512 m heap), `garage`
(+bootstrap), `caddy`, `searxng`, `aura-agent-memory-mcp`, `whatsapp`, `aura-pim-mcp`.
`markitdown` intentionally omitted (add later if document text-extraction is needed).

## Part 2 — Go unification (PROPOSED, needs rebuild)

Collapse the three mechanisms into one so `.env`/compose stay uniform:

1. **One route resolver** `CloudRoute(modality)` returning `(baseURL, apiKey, model)`
   used by embed, rerank, STT, TTS, and vision — one code path, one set of rules.
2. **Uniform env naming**: `AURA_<MODALITY>_MODEL` for every modality (alias the
   legacy `AURA_STT_CLOUD_MODEL`/`AURA_VISION_CLOUD` for one release).
3. **Uniform `/v1` handling** inside the resolver (callers never worry about the
   suffix; the resolver normalizes so appending the client's known suffix is always
   correct).
4. **Base compose forwards all knobs** to the `aura` service and stops hardcoding
   `AURA_EMBED_BASE_URL`/`AURA_RERANK_BASE_URL` to sidecar DNS (use a loopback
   default so the swap fires, matching the host-CLI default).
5. Tests: table-driven route resolution per modality (local + cloud + `/v1`
   normalization); update the env catalog + PRD amendment.

When Part 2 lands, `compose.cloud.yaml` shrinks to just the sidecar removal + Neo4j
tuning; the cloud model values move to a clean `.env` stanza.

## Follow-ups

- Object-store public endpoint (`AURA_OBJECTSTORE_PUBLIC_ENDPOINT`) still loopback —
  remote asset presign URLs won't resolve off-box until Caddy fronts garage or it's
  LAN-published.
- Verify STT with a real audio clip and memory-MCP cloud embed end-to-end.
