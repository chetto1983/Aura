# Completed broker sessions and shared OHLCV snapshots

The operator prioritized these two data-consistency changes after the broker 0.3.1 hardening.
The prior release and Aura invocation fix remain independently verifiable. No indicators,
learned model or generic cache manager are added.

## Evidence and limits

On the lab VM at 2026-11-03 15:46 UTC, each real Yahoo response was frozen once in memory,
then supplied unchanged to risk/technical/custom/portfolio. AAPL, ENI.MI and BTC-USD retained
November 3 in the first three reports and November 2 in portfolio. Their current daily bars
were incomplete. 7203.T retained November 2 everywhere, with Yahoo metadata naming the next
regular session on November 4. Yahoo's BTC regular-end value is 23:59 UTC, before daily rollover.
Evidence: `verification/2026-11-03-broker-cache-and-invocation-evidence.md`.

Code inventory: `data.history` drops timezone information and keeps the latest row; portfolio
alone removes the current UTC date. Existing portfolio archives contain closes, not full OHLCV.
yfinance 1.7.0 supplies current regular start/end, exchange timezone and latest quote time in
history metadata; its lazy `tradingPeriods` accessor performs another 5d/1h provider request.
The baseline proves selection disagreement, not the correctness of a future replay protocol.

## Session policy

- Ordinary price-based reports and snapshot acquisition default to
  `include_incomplete_session=false`. Add the explicit opt-in consistently to security risk,
  technical, custom, ETF and portfolio reports, and the internal FX-history paths they use.
- Use aware provider bar labels, observed-at time and applicable session metadata before
  normalizing daily indices for the existing calculations. A same-day equity bar becomes
  complete after its regular session end, including provider-reported early closes and DST.
- A crypto daily bar ends at the next UTC midnight, including weekends; 23:59 metadata does
  not make it complete early. Never include a future bar, even with opt-in.
- When metadata does not establish the latest bar's completion, retain only demonstrably
  complete rows and report uncertainty/exclusion. Do not invent exchange hours or calendars.
  Match the latest non-crypto candidate to the provider's applicable regular start/end,
  including sessions crossing midnight; a previous calendar date alone is not proof.
  Exclude an unmatched latest candidate, retaining the prior historical rows. In the measured
  Tokyo case, November 2 versus November 4 metadata is insufficient evidence, so this can
  conservatively lag one row. Surface that limitation rather than fabricate a close time.
- Expose `last_bar_timestamp` (the aware provider daily label), `last_bar_complete`,
  `include_incomplete_session`, and `completeness_reason`, plus excluded-candidate information.
  Separate a daily bar label from the latest trade timestamp and scheduled session close.
- Remove portfolio's separate UTC-date cutoff; all report paths use the common policy.
- Existing live-alert hooks explicitly request incomplete sessions, preserving that caller's
  existing live semantics. Fundamentals' current quote/statements are outside this daily-bar
  change. This exception is an implementation decision, not a claim the operator specified it.

## Explicit immutable snapshot acquisition

Add one tool, `market_snapshot`, using the existing content-addressed `audit.put/read` store.
It accepts a nonempty unique ticker set, period (default 3y), optional benchmark/base currency
and the session policy. Fetch all requested histories and required FX before publishing one
typed, versioned object. Return its SHA-256 `snapshot_id`, coverage, original retrieval and
session metadata, and enough acquisition parameters to reproduce later calls. Declare local
persistence honestly; ordinary read-only risk/technical tools remain read-only.

Store full normalized OHLCV, aware source labels/timezone, exact per-column numeric values
and provider metadata. Preserve integer volume precision; do not round through pandas JSON
defaults or coerce mixed columns through a floating `.values` matrix. Snapshot creation must
not expose the whole time series in the LLM result.
Capture the minimal ETF profile required by the existing report (`quoteType`,
`netExpenseRatio`, `totalAssets`, `currency`, `longName`, `shortName`, `fundFamily`) and its
retrieval provenance when the instrument is an ETF. Replay with missing required profile
data fails deterministically rather than calling `data.info`.

Capture directional FX series used by the current formulas: each ticker currency to the
requested portfolio base, and the benchmark currency to each requested ticker currency.
Base-only coverage is insufficient for every comparison. Missing pairs are errors on replay;
do not introduce cross-rate synthesis through another currency.

Risk, technical, custom, ETF and portfolio accept an optional `snapshot_id`. A supplied ID:

- reads only existing verified objects; no Yahoo call or silent extension/refresh;
- validates SHA, kind/schema, all ticker/benchmark/FX coverage and policy before calculations;
- rejects explicit conflicting periods/policies and missing series with useful domain errors;
- preserves acquisition-time completeness and provenance when the wall clock advances;
- may use different indicator/CAPM/confidence parameters, which do not change captured data.

Use `period=None` where necessary to inherit a supplied snapshot's period. Without an ID,
resolve the existing runtime defaults (risk 3y, technical/custom 1y, portfolio 5y, ETF 3y).
Report policy arguments use `include_incomplete_session: bool | None = None`: omission
inherits the snapshot policy, or resolves to false for live acquisition. Explicit mismatch
is an error. `market_snapshot` itself retains the boolean false default.
Existing fields and formulas remain. `snapshot_id` is distinct from portfolio's existing
audit record ID and archived snapshot hash; keep those objects and readers unchanged.
If a first snapshot lacks a benchmark or FX series, acquire a new broader snapshot explicitly.

## Storage and existing data

Reuse the existing selectors with precedence `BROKER_ANALYSIS_HOME > PLUGIN_DATA >
~/.broker-analysis`. `audit.root` stays the pure records-path selector; `hooks.home` reuses
its parent and retains directory creation. Read-only lookup must not create directories.
PLUGIN_DATA is already scoped per identity/source by Aura. No cross-identity/global cache.

Before updating the real plugin, preserve any existing old-home contents into the identity's
PLUGIN_DATA with byte/hash collision checks, without deleting or overwriting differing data.
Document the explicit one-time transfer. No permanent legacy lookup or migration daemon.

## Verification

- Session fixtures: before/after equity close, early close, DST, future rows, missing/stale
  metadata, next-session metadata, overnight cases and crypto midnight/weekends.
- Snapshot round trips preserve all OHLCV values/dtypes, labels, timezone and provenance.
- With Yahoo forbidden, replay succeeds across separate MCP sessions; malformed/corrupted IDs,
  wrong kinds, missing benchmark/directional FX/ETF profile and period/policy conflicts fail
  without network access. Omitted report period/policy inherit the captured values.
- Technical/custom close, SMA and RSI agree on one snapshot; SPY/SPY invariants, zero stress,
  component ES sums and archive integrity retain their existing coverage.
- Home precedence, pure read lookup and identity-root separation get explicit fixtures.
- Keep the lazy 0.3.1 startup contract; discovery now exposes 15 tools without heavy imports.
- Run full offline suite/lint/build, selected real transport/provider checks, actual Aura
  acquisition/replay and measured cross-tool agreement before release. Separate live quote
  revisions and runtime latency from numerical equivalence on frozen inputs.

## Deferred scope

Uniform structured application-error envelopes, exact reasons for every unrelated null,
longitudinal latency/provider/calculation metrics and a cross-record monitoring summary remain
separate medium-priority work. Existing invariant tests are retained rather than duplicated.

## November 5 functional review amendment

The operator's follow-up snapshot `fe370e40...` was inspected on the live VM. Its AAPL/SPY
metadata excludes November 4, retains November 3 and incorrectly associates November 5's
regular close with the retained row. A fresh AAPL provider request reproduced this mismatch.
The installed yfinance public lazy `history_metadata.get("tradingPeriods")` returned a
five-day DataFrame containing November 4's actual regular start/end, including its -05:00
offset, in 0.124 s in this cached sample. No new calendar dependency or network adapter is needed.

For new acquisitions, request that existing schedule only when the latest non-crypto bar
cannot be matched to the current regular session. Match its exchange-local date to the
provider schedule's date; freeze the minimal dated start/end evidence with the OHLCV.
`session_end_timestamp` describes the selected bar's matched session or is null when unknown;
it never borrows a different day's close. Missing schedule evidence retains the conservative
fallback. Replay of already stored snapshots preserves their original values and metadata;
new schedule evidence is strictly validated and replay never fetches it.

Real stdio MCP on 0.4.0 also reproduced five lower groups failing with chart=false, SMA1000
returning null without an insufficient-history warning, duplicate SPY/SPY exclusion warnings,
and a nonexistent snapshot exposing its absolute storage path. Scope the pane limit to chart
rendering while preserving numerical/spec validation and the chart=true limit. Add a specific
custom-series warmup warning when the configured lookback exceeds valid source history.
De-duplicate identical report warnings without merging distinct source evidence. Sanitize
expected audit read errors without weakening hash checks or leaking paths. These are measured
functional defects, not evidence about independent formula accuracy, load or production uptime.
