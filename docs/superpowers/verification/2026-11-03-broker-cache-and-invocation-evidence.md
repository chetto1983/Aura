# Broker cached startup and invocation evidence — 2026-11-03

## Scope and provenance

The operator requested implementation after the review of VM conversation
`01a1a1c9-ba15-70f2-9cce-ac701c3d646d`, then asked why a cached broker takes about 30 seconds
to start. The lab VM is `192.168.101.158`; no cache was removed. All portfolio inputs below
are synthetic. Times are seconds unless stated otherwise.

- Broker server source: `edbb0312c97aaf22900406e01d112a99fe1296b6`.
- Broker packaging/release: `0525e82f57229bbc14efecfc244227d2e8a99c82`, tag `v0.3.1`.
- Aura reconnect: `047d0f092`; invocation lifecycle: `e7663740a3893d69c90c7e10c27dd13683a14b24`.
- Broker CI on the packaging SHA: run `41661759306`, success.
- Authenticated plugin upgrade returned applied=true, 0.3.0/9886eb3 to 0.3.1/0525e82,
  with the connector remounted and five unchanged skills.

## Why the existing cache did not prevent the delay

The identity's box mounts both uv caches: 1.5 GB at `/root/.cache/uv` and 112 MB of managed
Python at `/root/.local/share/uv`. Python 3.14.6 was already there. The box has a 2 GB RAM
limit and a two-CPU quota on an eight-CPU VM.

1. `uvx --from git+...@v0.3.0 python -c pass` still resolves the Git tag online: 3.700 s.
   The same full-commit source takes 0.476 s. The tag fails offline despite cached packages;
   the full commit initializes offline successfully. uv documents its Git cache by resolved
   commit: <https://docs.astral.sh/uv/concepts/cache/>.
2. A new Python process imports modules again. One original server import took 12.457 s,
   with importtime attributing 11.639 s to the import tree: analysis 8.432 s, including
   data/pandas/yfinance, plus workbench/charts 1.435 s. A package cache is not a running
   interpreter or a cache of imported module objects.
3. The existing reconnect path discarded the box's configured 120-second initialization
   budget. Logs show three 10-second failures, 31.5 s elapsed, then a 30-second breaker
   cooldown. Real SDK-session TDD reproduced 9.999995 s received instead of 120 s.

The fixes reuse Python's native module cache through function-local imports, pin the uv source
to its full immutable commit, and reuse `withFirstStart` for every identity-child session open,
including reconnect. No cache manager, protocol implementation or background warm-up service
was added. Identity and worker sessions remain separate.

## Measured startup after the fix

The existing `spikes/agent-browser-auth/mcp_cold_start.py` sends initialize and tools/list.
It records JSON-RPC stdout noise separately. All successful measurements below return all
14 tools with no stdout noise.

| Launch | Initialize | Tools/list |
|---|---:|---:|
| Original source, full-commit environment, adjacent comparison | 5.91 | 5.92 |
| Candidate lazy imports, same environment | 1.87 | 1.88 |
| Candidate repeat | 2.08 | 2.09 |
| Installed 0.3.1, first sampled start after upgrade | 3.42 | 3.44 |
| Installed 0.3.1, `uvx --offline` | 2.09 | 2.10 |
| Installed 0.3.1, subsequent online start | 1.61 | 1.61 |

The actual installed `/plugins/broker-analysis/mcp.json` supplied the final launch command;
the source is `git+https://github.com/chetto1983/broker-analysis-mcp@edbb0312c97aaf22900406e01d112a99fe1296b6`.
The authenticated MCP board probe returned HTTP 200, ok=true, tool_count=14.

These are sequential samples on one loaded VM with warm dependency caches, not percentiles
or an empty-cache benchmark. First calculation still imports its numerical/provider modules.

## Installed broker checks

The real stdio MCP client used the installed manifest in the identity's box, package version
0.3.1, with a separate scratch audit home.

- Confidence 1.2: precise `confidence < 1` schema error, 24 ms in the final run.
- RSI period zero: precise `rsi_period >= 2` schema error, 8 ms.
- ETF period 1d: enumerated supported periods, 7 ms.
- AAPL with both analyst CAPM rates: unavailable debt cost, WACC and EVA each report
  `missing_inputs=[interest_expense]` and a matching warning, with schema and retrieval metadata.
- SPY 6,000 USD + QQQ 3,000 USD, NAV 10,000 USD: residual cash 1,000 USD; portfolio result
  saved and read back exactly. `analysis_record` returns the saved output before its audit
  envelope is attached, as the existing documented hash contract requires.
- An initial ad-hoc equality assertion incorrectly included that audit envelope. It was
  corrected against `audit.save` and the existing live persistence test; no product change
  was needed for that harness error.

The local suite passes 258 offline tests and Ruff; wheel/sdist builds pass. Eleven review-pass
regressions cover hook catalog validation before Yahoo/persistence and finite chart levels.
No hook was created, executed or removed on the VM.

## Aura validation and review

- Native WSL `go vet ./...` and `go build ./...`: pass. Final changed-scope vet/application
  builds, lint, deadcode, file-size and native precommit checks also pass.
- Full race suites pass for mcptools, gateway, agent, agent/tools and runner.
- mcptools unit coverage: 93.6%. Real disposable PostgreSQL `db_integration` coverage:
  gateway 90.2%, agent 89.8%, runner 87.1%, combined 88.8%; tools unit coverage 89.9%.
  The disposable database ran the real migration chain; no live database migration was added.
- Scoped mutation probes: reconnect 1/1 killed; ledger 8/8 killed; background 2/2 killed.
- Independent review approved reconnect. Its three Important combined-review findings were
  fixed by TDD: actual background completion, hook preflight and finite chart levels.

The gateway now carries immutable actual arguments and start time to its delegated end.
Read retries/backoff belong to one dispatch duration. A background promotion is an interim
notice: the existing BackgroundCalls terminal seam closes the ledger and operation claim
only when raw execution settles, including cancellation/drop. Parent Runner end ownership,
reservation replay protection and first-writer behavior remain.

## Decisions and boundaries

- An earlier suggestion to mark audit writes closed-world would misrepresent Yahoo reads.
  The broker keeps truthful open-world/non-idempotent hints and a separate local-write class.
  Aura's conservative generic fallback and existing standing approval remain. The chart-saving
  custom tool now honestly declares writes; this is conservative even with chart=false.
- The existing shared reconnect policy detaches the initiating caller's cancellation. It can
  keep restoring a session for the configured 120 seconds; this change retains that policy.
- These checks establish integration, diagnostics and observable consistency, not independent
  financial formula accuracy, other identities, arbitrary providers, or outage percentiles.

## Final daemon acceptance

The authenticated updater applied Aura `e7663740a3893d69c90c7e10c27dd13683a14b24`; `/readyz`
returned HTTP 200, ready=true. The edge, ArcadeDB MCP, create-aura, CodeQL and Skills workflows
passed on this exact SHA. The main CI's remaining long mutation scope is tracked separately;
no success is inferred from it still running.

Real-agent conversation `01a1a20f-5f15-7542-9683-de347f183aff` completed successfully in
61.19 seconds, with two independently dispatched workers and the parent checking SPY/SPY.

| Actual call | Status | Duration ms | Arguments retained | Start/end duration agrees |
|---|---|---:|---|---|
| Worker catalog | ok | 2,350 | yes, start and end identical | yes |
| Worker AAPL fundamentals | ok | 4,053 | yes, start and end identical | yes |
| Parent SPY/SPY risk | ok | 2,410 | yes | parent timing convention unchanged |

The SQL aggregate over the two worker broker calls returned count=2 and true for all four
checks: status ok, positive duration, exact start/end arguments, and duration within 2 ms of
the persisted start/end interval. Both reports were delivered and the final answer correctly
reported 12 catalog indicators and the three explicit missing-interest diagnoses. SPY/SPY
returned beta/correlation 1, tracking error 0 and alpha 0.

Acceptance score: **10/10** over exact deployed revision/readiness, installed version/pin,
14-tool discovery, invalid-input diagnosis, capital-null diagnosis, portfolio persistence,
SPY identity invariants, each worker's actual invocation evidence, and successful final delivery.
This is the named integration scenario's binary rubric, not a financial-model quality score.

## Subsequent request: completed sessions and shared data

The operator then prioritized incomplete daily bars and reusable snapshots. The current risk
reply ended on 2026-11-03, while portfolio risk's UTC-date cutoff ended on 2026-11-02.
A separate real-provider baseline at 15:46 UTC froze each Yahoo OHLCV frame once and passed
that identical frame to the existing report functions, with audit publication disabled:

- AAPL and ENI.MI: risk, technical and custom retained November 3 during their still-open
  regular sessions; portfolio retained November 2.
- BTC-USD: same discrepancy. Yahoo's regular-end metadata says 23:59 UTC, so it is not itself
  proof that a crypto daily bar is complete before the next UTC midnight.
- 7203.T: all retained November 2. The latest trade was November 2 and metadata named the
  next regular session on November 4, demonstrating that session metadata need not name the
  last observed bar's date.

Only known metadata keys were read; the lazy `tradingPeriods` key was avoided because it
performs another history fetch. These measurements establish the selection discrepancy,
not a shared-snapshot implementation. That follow-up has its own design and plan.
