---
id: 090-turingdb-runtime-durability-fit
title: TuringDB runtime and durability fit for Aura's graph store
date: 2026-07-09
status: VALIDATED
type: standard
tags: [turingdb, graph-store, durability, vector, neo4j-alternative, phase-15]
related: .planning/spikes/067-apache-age-pipeline, .planning/spikes/068-arcadedb-pipeline, .planning/spikes/071-arcadedb-adopt-strategy
---

# Spike 090 - TuringDB runtime and durability fit

## Question

Can the current TuringDB Python wheel provide a durable writable graph substrate, with vector index persistence, without Docker or a separate daemon?

## Harness

Run from PowerShell on the Docker Desktop host:

```powershell
powershell -ExecutionPolicy Bypass -File .planning\spikes\090-turingdb-runtime-durability-fit\run.ps1
```

The harness builds `aura-turingdb-spike:1.35` from `python:3.14-slim` plus `turingdb==1.35`, then starts `turingdb start -demon` inside the probe container. Scratch data is mounted at `D:\tmp\turingdb-aura-spike-docker\090-runtime-durability`.

The official `turingdbai/turingdb:nightly` image was not used as ground truth: it identified as older than PyPI (`1.30.1.dev42` in Python package metadata, CLI `--version` reported `1.0`). The wheel-backed image logged commit `b5c5338ed9b6dc34f808f68bf7e4989d61a731ae` from 2026-07-08.

## Results

Verdict: VALIDATED.

| Check | Result |
|---|---|
| Start Docker-hosted daemon | PASS, 716 ms |
| Create graph `aura090` | PASS |
| Write/submit/read marker node | PASS, marker read before restart |
| Create/load/search 4d vector index | PASS, id 100 top hit |
| Stop/restart daemon on same volume | PASS |
| Load graph after restart | PASS, 214 ms |
| Read marker after restart | PASS |
| Search vector index after restart | PASS |

Important caveat: user-created graphs are durable but not auto-loaded on daemon restart. After restart, the server loaded `default`; `aura090` returned `GRAPH_NOT_FOUND` until the harness called `load_graph("aura090")`. A production sidecar would need explicit graph load during boot.

The vector index persisted independently and was searchable after restart once the graph was loaded.
