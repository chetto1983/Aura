# Broker 0.4.1 functional follow-up - November 5, 2026

## Reproduced baseline

The operator's review was checked against the installed 0.4.0 server, through real stdio MCP
in the identity's VM box. Snapshot
`fe370e406e6e23f8e89609e095aaffb8b2128e448dff371d47b7f940cf8d306b` retains November 3,
excludes November 4 and associates November 5's regular end with that retained bar.
A fresh AAPL daily request reproduces the metadata mismatch. The installed yfinance 1.7
public lazy `history_metadata.get("tradingPeriods")` returns a dated November 4 regular
session, 09:30-16:00 America/New_York, in a cached 0.124-second sample.
The public implementation is in
[yfinance 1.7 history.py](https://github.com/ranaroussi/yfinance/blob/1.7.0/yfinance/scrapers/history.py).
Its formatter derives the historical schedule index from the start date; that index alone
does not establish an overnight daily-bar association.

The same real MCP probe reproduces: five lower groups rejected with chart=false; SMA1000
null without a history warning on 751 bars; two identical SPY/SPY exclusion warnings; and
an absolute PLUGIN_DATA path exposed for a nonexistent object. No graph or hook was executed.
These observations were recorded before code in Aura amendment `5eb357a62`.

## Source, regressions and independent review

- Session implementation `683ca18`: session/snapshot regressions observed RED then GREEN;
  full offline suite 327 passed, 26 live deselected; Ruff/diff-check passed.
- Numeric/chart/diagnostic implementation `62fada1`: corresponding regressions RED then GREEN;
  full suite 336 passed, 26 live deselected; Ruff/diff-check passed.
- Version preparation `e63fb4e` declares 0.4.1 consistently in package, lock and three manifests.
- Independent read-only review found three Important issues: start-indexed overnight rows
  could attach the next day's close; schedule dates used their own offset rather than exchange
  time; replay checked applicable completion only for the final retained bar. No Critical or
  Minor findings were returned.
- One TDD fix pass `e059c52643ba2224000b29efadd9e115123993a3` covered all three findings:
  `test_start_indexed_overnight_schedule_does_not_assign_next_days_close`,
  `test_replay_rejects_schedule_on_another_exchange_local_date`, and
  `test_replay_rejects_unclosed_schedule_evidence_for_a_nonfinal_retained_bar` all failed
  before their fixes. New dated evidence requires the same exchange-local start/end date;
  ambiguous overnight rows are omitted during acquisition. Strict replay validates all
  retained labels and applicable boundaries. Legacy snapshots without new evidence retain
  their frozen metadata. Existing nested current/regular shape validation is retained.
- Final offline suite: **339 passed, 26 live deselected, 27.07 seconds**. Ruff, diff-check,
  wheel and sdist build passed. The excluded live tier is not claimed green; selected real
  provider/transport checks follow below. Packaging-only regressions subsequently passed 5/5.

Packaging `9d5d637915cc4486707229604594094aa2467829` pins both launchers to full source
`e059c52`. Main [CI 41996879593](https://github.com/chetto1983/broker-analysis-mcp/actions/runs/41996879593)
and tag [CI 41997134099](https://github.com/chetto1983/broker-analysis-mcp/actions/runs/41997134099)
both passed on that packaging SHA; annotated `v0.4.1` points there.

## Actual VM candidate and installed package

The exact built 0.4.1 wheel acquired AAPL/SPY, 3y, benchmark SPY, USD, completed-only on
the VM at 08:45 UTC. New snapshot:
`68d0b8d65aa40b4c8b097d6c3d57713bc7350d152b175a44b0996cb5359a8015`.
Both sources retain **November 4, complete=true**, with session end
**2026-11-04T16:00:00-05:00**, rather than November 5. Acquisition took 15.990 seconds,
including first calculation/provider imports and requests; this is not server startup.

Two fresh child MCP processes then replayed with Yahoo/history/FX/profile seams forbidden:

- All five report paths share the captured ID and end November 4 with correct session ends.
- Standard/custom agree for close, SMA50, RSI14, MACD components, Bollinger bounds and ATR14.
- Five automatic lower groups succeed with chart=false. The attempted chart=true equivalent
  fails before rendering; no graph is generated. SMA1000 stays null with a specific warning.
- `close > 0` state events cover every returned bar, with identical after-event/general
  statistics and zero difference t statistic; this remains an event study, not a backtest.
- SPY/SPY identity invariants hold. The old snapshot's repeated exclusion warning appears once.
- The nonexistent ID returns a domain error without absolute paths or JSON filenames.
- The old full technical report remains exactly hash-equivalent to the measured 0.4.0 response:
  `8ce6c2d1911c878d19f9c66df654615d871674c729cfbf505fad63905763a49a`.
  Its original November 3 end and November 5 metadata remain frozen. New evidence requires
  a new snapshot; no historical object is rewritten. This is not equality of old/new datasets.
- The earlier 0.3.1 legacy record still reads back with output hash
  `664400f2b54334b9614b95230ff972a032c862d0ea8c0e4618f1491aeddae4fb`.

By the authenticated Governance upgrade request the plugin already reported 0.4.1/`9d5d637`:
HTTP 200, up_to_date=true, applied=false. This request is not claimed to have performed the
upgrade. The actual installed manifest selects `e059c52`, package metadata reports 0.4.1,
and the board probe returns HTTP 200/ok/15 tools. The installed Git-sourced package replays
the same ID in two more fresh provider-forbidden processes. All checks pass with the same
stable summary digest as the candidate wheel:
`5f39fcbaf33415f70d2134acfb93b8b1f4f77d94a229af9947dcedc72d6421a0`.

Installed-manifest startup: initialize 2.79 s / tools-list 2.80 s online; 1.28 s / 1.28 s
with uv offline. Both expose 15 tools without stdout noise. These are sequential cached
samples on one VM, not cold-cache or latency-percentile measurements. Installed successful
report calls in this sample take 0.201-0.410 s; legacy record reads take 0.007 s.

## Real Aura agent

Conversation `01a1aae6-cd2f-724e-b3cb-556d354ab110` executes the old technical/risk replay,
new acquisition, new technical/custom replay, and the expected missing-ID failure. Its new ID:
`38f569371b1a74e422fe06aa03906bb488a93018fdac83317c30642a0424a069`.
Read-only DB evidence shows two new report calls, status ok, positive durations, one shared
ID and omitted periods. Custom uses chart=false, seven indicators with automatic panes and
the lower `gap` expression; the five-group numeric request succeeds. No approval pause occurs.

The initial stream ends with `context canceled` at 08:51:36 UTC, after the tool calls but
before a final answer. The persisted interruption placeholder is not treated as delivery.
This coincides with an external Aura container restart: StartedAt 08:51:48 UTC, and the
system API then reports running revision `7a71027c4`, ready/current, zero live runs. No
HTTP cancel/update claim appears in the queried 08:48-08:53 interval. These observations
do not establish who or what initiated the restart.

Resume in the same thread completes successfully in 20.30 seconds; persisted assistant
sequence 30 confirms the frozen old window, new November 4 window/session end, standard/custom
agreement, successful numeric five-group request, specific SMA1000 warning and path-free
missing-ID error. It honestly declines the old risk warning count because that result is no
longer readable in its persisted context. A focused reread, without another acquisition,
then completes in 15.05 seconds: risk call status ok/duration 2,165 ms, persisted assistant
sequence 34 explicitly confirms one Excluded warning on the original window. The interrupted
attempt and both successful continuations are recorded separately, not collapsed into an
uninterrupted success. `/readyz` finally returns HTTP 200/ready=true.

| Actual initial tool call | Ledger status | Duration ms |
|---|---|---:|
| old technical replay | ok | 174 |
| old SPY/SPY risk replay | ok | 217 |
| new AAPL/SPY market_snapshot | ok | 1,380 |
| new technical replay | ok | 534 |
| numeric custom, five lower groups | ok | 1,116 |
| nonexistent snapshot, expected failure | error | 60 |

Named functional acceptance is complete for this scenario. It includes final delivery after
the external restart and explicit reread; it does not establish uninterrupted operation.

## Limits

Same-local-day provider evidence fixes the measured AAPL/SPY case. Historical overnight
association remains conservative when the daily label cannot be established; current-period
fallback retains the existing closing-date fixture. Independent formula implementations,
provider failure/load tests, continuous-operation guarantees, complete null taxonomies and
longitudinal gateway/provider/calculation telemetry remain separate work. This is functional
acceptance of a prototype using unofficial Yahoo data, not production certification.
