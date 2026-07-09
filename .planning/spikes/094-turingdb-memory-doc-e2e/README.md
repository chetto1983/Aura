# 094 - TuringDB Memory + Document E2E

## Question

Can the current Dockerized TuringDB wheel support the two Aura behaviors the
operator asked for: agent memory store/retrieve and document ingest/retrieve,
with a machine-checked E2E score above 9.8/10?

## Method

`run.ps1` / `run.sh` run `probe_e2e.py` inside the wheel-backed
`aura-turingdb-spike:1.35` image. The probe:

- records the installed `cli-printing-press` version from the host runner;
- starts a TuringDB daemon on a mounted scratch volume;
- writes a scoped agent-memory graph (`User -> Memory`) and vector index;
- writes document/chunk graph data (`Document -> Chunk -> NEXT_CHUNK`) and a
  separate vector index;
- validates identity-scoped memory retrieval;
- validates document chunk retrieval plus next-chunk context expansion;
- restarts TuringDB, explicitly reloads the user graph, and reruns retrieval;
- computes a strict 10-point score from machine checks.

## Score Gate

The score is not an LLM judgement. It is `earned_points / total_points * 10`
over ten 1-point checks. The runner exits non-zero unless `score >= 9.8`.

Current required checks:

1. `cli-printing-press` installed at `>= 4.28.0`.
2. TuringDB graph and vector indexes created.
3. Agent memory scoped graph written.
4. Alice memory query retrieves exact top-1.
5. Memory identity scope keeps Bob's decoy out of Alice's result.
6. Document graph and chunk chain written.
7. Document retrieval returns exact top-1 chunk.
8. Retrieval expands the next chunk as graph context.
9. Memory and document retrieval p95 stay under 250 ms in the sample.
10. Restart durability preserves graph and vector retrieval.

## Run

```powershell
.planning\spikes\094-turingdb-memory-doc-e2e\run.ps1
```

Linux/macOS with Docker:

```bash
bash .planning/spikes/094-turingdb-memory-doc-e2e/run.sh
```

## Result

2026-07-09 run:

- Verdict: `VALIDATED_9_8`
- Score: `10.0/10`
- `cli-printing-press`: `4.28.0` on Go `1.26.5`
- TuringDB client/package: `1.35`
- Memory retrieval sample: p95 `3.763 ms`
- Document retrieval sample: p95 `3.620 ms`
- Restart durability: memory top-1 and document top-1 still exact after
  daemon stop/start plus explicit `load_graph("aura094")`

See `results.json` and `run.log` for the full machine evidence. A validated
result proves the custom-port path works for these two behaviors; it does not
make TuringDB a Neo4j drop-in for the existing Aura memory/document Cypher.
