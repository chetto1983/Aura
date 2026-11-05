# Broker completed sessions and immutable snapshots — 2026-11-03

## Baseline and contract

The operator prioritized completed daily sessions and a shared snapshot before adding indicators.
The real-provider baseline at 15:46 UTC is recorded in
`2026-11-03-broker-cache-and-invocation-evidence.md`: AAPL, ENI.MI and BTC-USD used different
last rows across report paths despite receiving the same frozen frames. Tokyo metadata named
a different session from its latest observed bar. That baseline does not prove replay correctness.

Design/plan and the measured PRD amendment were committed as Aura `fb49e0aa2`.
The two implementation tasks are in the separate broker repository. No Aura runtime code,
indicator, calendar dependency or second storage engine is required by this follow-up.

## Decisions established before release

- Reports resolve an omitted session policy to completed-only on live data, or inherit the
  snapshot policy. Explicit period/policy conflicts fail without fetching.
- A latest non-crypto bar needs applicable regular-session evidence. Unmatched latest bars
  are excluded with uncertainty, retaining earlier observed historical rows. The measured
  Tokyo case can conservatively lag one row; no exchange calendar is invented.
- Crypto daily bars become complete at the next UTC midnight. Future bars stay excluded
  with or without explicit incomplete-session opt-in.
- Acquisition captures full OHLCV, aware labels, minimal ETF profile and directional FX
  coverage needed by the existing formulas. Replay reads the verified audit object only.
- The existing audit store uses `BROKER_ANALYSIS_HOME > PLUGIN_DATA > ~/.broker-analysis`.
  Old-home files must be preserved with collision/hash checks before the real plugin upgrade.

## Completed-session implementation

Broker commit `142442756e794c228c822a240d73f84133b374df` passed 275 offline tests,
with 26 live tests deselected, Ruff and diff-check. New fixtures cover ordinary/early close,
DST, next-session and overnight metadata, exact crypto midnight, future rows and hooks'
explicit live opt-in. Portfolio's separate UTC-date cutoff is removed.

Independent review found an Important provenance gap: risk/ETF reported only primary-series
completion even when an aligned benchmark or FX series was incomplete. The finding was
accepted for a TDD fix; the initial commit's green suite is not evidence that this gap is fixed.

## Remaining acceptance boundary

At 15:59–16:00 UTC the VM ceased responding to SSH and host reachability checks, before
the planned one-time home transfer. No transfer or 0.4 plugin upgrade is claimed here.
Source/test/transport results and the eventual release/VM checks must be recorded separately.

## November 5 resume

After the operator restarted the VM, SSH responded and authenticated Governance confirmed
Aura `e7663740a` and broker 0.3.1/`0525e82`. The one-time preservation inventory selected
`/root/.broker-analysis` and the verified identity source root
`/workspace/.plugin-data/broker-analysis-7011f7f24e82`: the old home contained zero files,
so zero were copied or overwritten. The source was retained. This establishes the current
inventory only; it does not recover or certify archives from previously removed containers.

A synthetic SPY/NAV 1,000 USD portfolio was then archived through the installed 0.3.1 MCP
server specifically for the upgrade continuity check. The collision-checked transfer copied
six files (129,266 bytes), verified every source/destination SHA-256 and retained the source.
Its sorted path/hash manifest SHA-256 is
`2efc4758dc568661fa2c642de29a6268bb5cf4ffe03680170a4a5c3a6d063dca`.
The record ID is `f1b8e8f92955a85c4224413659ec89c834b79646cb35dd34a35f344b130f284e`;
its output hash is `664400f2b54334b9614b95230ff972a032c862d0ea8c0e4618f1491aeddae4fb`.
Reading that same record through the upgraded server remains a separate acceptance step.

## Final source, review and offline checks

Broker runtime/source `831e46695029d6e8a7e028c0eb5f09cd818a8077` includes the coherent
snapshot implementation `2694c00`, version preparation `af1ecb8`, and the review fixes.
Packaging `84d01b1c44e929f9e95f6b1a72679b716932c5e5` pins both launchers to that full source
SHA. Main CI run `41989996446` passed on the packaging SHA.
Tag `v0.4.0` points to the same packaging commit; tag CI `41991229490` also passed.

Final independent root verification: 316 passed, 26 live deselected, 21.87 seconds in the
existing native Linux venv; Ruff passed; offline wheel and sdist for 0.4.0 built successfully.
The live tier is exercised below rather than inferred from its exclusion in offline CI.

Independent review accepted provider-free routing and identified one Important persisted-shape
validation gap and one Minor FX label-alignment gap. Both were fixed through failing regressions:
strict Meta/ETF profile types, policy/observation/retrieval/session-time consistency, malformed
session maps, and the aware benchmark label after leading FX rows are removed. No second
storage engine, calendar dependency or indicator was introduced. Portfolio retrieval metadata
and ETF DST warnings now retain acquisition-time provenance during replay.

## Real provider and immutable replay

Before upgrading, the exact built 0.4.0 wheel ran through real stdio MCP in the identity's VM
box, using its persistent PLUGIN_DATA. Acquisition fetched AAPL, ENI.MI, SPY, BTC-USD and
the required directional FX, plus SPY's minimal profile. A second acquisition opted into
incomplete sessions for AAPL/BTC. All five reports then passed in two fresh child processes
with the Yahoo/history/FX/profile access seams explicitly forbidden.

- Completed-only snapshot: `685bac619607131552da56b0c81c61a6689a2a49bf5ce2341aaf481b3e696093`.
- Opt-in snapshot: `0ef151cd8a9d810f9ff1d875f12f685d39cc90210bde8850b0b311140ea04792`.
- Standard/custom close, SMA50 and RSI14 agree exactly; SPY/SPY beta/correlation are 1,
  tracking error and Jensen alpha are 0; ETF tracking difference/error are 0. FX-based
  comparison and a synthetic mixed-currency portfolio replay with the same snapshot ID.
- BTC closed-only ends November 4, complete=true; opt-in retains November 5, complete=false.
- AAPL's provider metadata names a later session: default retains November 3, complete=true;
  opt-in retains November 4, complete=null. This demonstrates the documented conservative
  one-row lag when metadata cannot establish the latest observed bar's completion.
- An explicit conflicting policy returns the expected domain error. No provider fallback occurs.
- The legacy 0.3.1 record reads back with its exact original output hash in both new processes.

The first multi-instrument acquisition took 23.877 seconds, including initial calculation/provider
imports and provider requests; the subsequent two-instrument acquisition took 0.526 seconds.
These are tool-call durations, distinct from server startup and not longitudinal percentiles.

## Installed source acceptance

Authenticated Governance returned HTTP 200, applied=true, 0.3.1/0525e82 to 0.4.0/84d01b1,
with the connector remounted. The board probe reports 15 tools and `/readyz` reports ready=true.
The installed manifest selects source 831e466; its Python package reports version 0.4.0.

The existing `spikes/agent-browser-auth/mcp_cold_start.py` measured the actual installed
manifest: initialize 3.78 s / tools-list 3.79 s online; 2.33 s / 2.33 s with uv offline.
Both return 15 tools with no stdout noise. These are sequential cached samples on one VM.

The installed Git-sourced package then replayed the already acquired IDs in two more fresh
provider-forbidden MCP processes. Every assertion passed, including the legacy record.
Its stable summary digest equals the candidate wheel's:
`b36ae7d27b322dd9e992aa36488282e4a0c0402d5581ef79d7343d6d0c9b36eb`.
Successful report calls took 0.286–1.244 seconds in these samples; legacy readback took
0.012–0.018 seconds. This does not separate all transport, deserialization and calculation spans.

## Real-agent completion

Conversation `01a1aab3-80b3-756a-9c94-7b45760c0d33` first paused at the gateway for the new
local-write tool. The pending approval was resolved outside the test driver with the existing
`gateway_scope:always:broker-analysis__market_snapshot` option. No broker-name exception,
false closed-world hint or classifier change was added. The original withheld intent (29 ms)
is distinct from the later actual acquisition.

| Actual tool execution | Status | Duration ms |
|---|---|---:|
| market_snapshot, AAPL/SPY, 3y, completed-only | ok | 3,482 |
| technical_indicators, AAPL | ok | 124 |
| custom_analysis, AAPL, chart=false | ok | 236 |
| risk_metrics, SPY/SPY | ok | 338 |

The read-only SQL aggregate returned three report calls, all status ok and positive duration,
one distinct snapshot ID, and no explicit period argument in any report call. That ID is
`9edb95c36f98450bd8cd9423a405fcbf66c4dbd5f82aead85dfb7939c965d94f`.
The final persisted assistant message (sequence 17) reports identical standard/custom close,
SMA50 and RSI14, SPY/SPY identity invariants, the captured window and the explicit exclusion
of November 4 because its completion was not established. The initial SSE pause alone was
not treated as final delivery; the resumed execution and persisted answer were checked.

Named integration acceptance: **10/10** for installed version/source, discovery, completed-only
and opt-in selection, strict/frozen metadata, all-five-tool shared data, numerical equivalence,
identity/FX checks, provider-forbidden process restarts, exact legacy readback and real-agent
delivery/ledger evidence. This is a binary rubric for this scenario, not financial-model quality.

## Remaining scope

Uniform global application-error envelopes, complete reasons for unrelated nulls, and
longitudinal gateway/provider/calculation telemetry remain the operator's medium-priority
follow-up. Startup timings above are cached sequential samples, not outage/percentile evidence.
The conservative metadata policy can omit one already closed row; that uncertainty is explicit.
