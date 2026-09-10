// analytics.go is the three reconciliation calls COVERAGE.md reclassified INTEGRATE for
// plan 02-09: ListKeys (GET /api/v1/keys, the whole roster in one call), GetCredits
// (GET /api/v1/credits, the account pool) and AnalyticsQuery (POST
// /api/v1/analytics/query, per-identity everything). All three require the MANAGEMENT
// credential and are server-side only (COVERAGE.md, T-02-11) — the browser never calls
// them directly.
//
// Field names are transcribed from 02-OPENROUTER-API.md (retrieved from openapi.json on
// 2026-09-08), same discipline as client.go/wire.go: never from recall. The analytics row is
// decoded here rather than through the official Go SDK because v0.7.129 types it as an
// empty struct (QueryAnalyticsData1, models/operations/queryanalytics.go), which drops every
// metric the provider sends.
package openrouterprovision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ListKeys reads the whole key roster in one call (GET /api/v1/keys). Same record shape
// GetKey decodes (keyDataWire, minus the raw key), reused here rather than declaring a
// second decoder for the identical wire shape (CLAUDE.md REUSABLE CODE).
func ListKeys(ctx context.Context, client *http.Client, baseURL, apiKey string) ([]KeyRecord, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/keys", nil)
	if err != nil {
		return nil, fmt.Errorf("openrouterprovision: list keys: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouterprovision: list keys: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openrouterprovision: list keys: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, classify(resp.StatusCode, respBody)
	}

	var wireResp struct {
		Data []keyDataWire `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, fmt.Errorf("openrouterprovision: list keys: decode response: %w", err)
	}
	out := make([]KeyRecord, 0, len(wireResp.Data))
	for _, w := range wireResp.Data {
		out = append(out, keyRecordFromWire(w))
	}
	return out, nil
}

// Credits is GET /api/v1/credits's decoded pool. Remaining is total_credits minus
// total_usage — computed by the CALLER (the over-allocation banner needs it alongside a
// separately-summed Σ(cap), so this package does not guess at what the caller wants to do
// with the difference).
type Credits struct {
	TotalCredits float64
	TotalUsage   float64
}

type creditsWire struct {
	TotalCredits float64 `json:"total_credits"`
	TotalUsage   float64 `json:"total_usage"`
}

// GetCredits reads the account-wide credit pool (GET /api/v1/credits). Every per-key cap
// allocates from this ONE pool and OpenRouter does not stop their sum from exceeding it
// (M-12) — this is the figure the over-allocation banner compares Σ(cap) against.
func GetCredits(ctx context.Context, client *http.Client, baseURL, apiKey string) (Credits, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/credits", nil)
	if err != nil {
		return Credits{}, fmt.Errorf("openrouterprovision: get credits: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		return Credits{}, fmt.Errorf("openrouterprovision: get credits: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Credits{}, fmt.Errorf("openrouterprovision: get credits: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Credits{}, classify(resp.StatusCode, respBody)
	}

	var wireResp struct {
		Data creditsWire `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return Credits{}, fmt.Errorf("openrouterprovision: get credits: decode response: %w", err)
	}
	return Credits{TotalCredits: wireResp.Data.TotalCredits, TotalUsage: wireResp.Data.TotalUsage}, nil
}

// ErrTimeRangeMissingSeconds marks a time_range bound that is not a full RFC3339 instant.
// 02-OPENROUTER-API.md: "time_range: { start, end } — ISO 8601 UTC, seconds REQUIRED
// (minute precision is rejected)". time.Parse(time.RFC3339, ...) is the local guard: RFC3339
// mandates the seconds field, so a minute-precision string ("2026-09-08T10:00Z", no ":00"
// seconds group) fails to parse against it and is refused HERE rather than shipped as a
// provider 400 an operator has to decode.
var ErrTimeRangeMissingSeconds = errors.New("openrouterprovision: time_range must be a full RFC3339 instant with seconds; minute precision is rejected by the provider")

// TimeRange is POST /api/v1/analytics/query's time_range object. Start/End are the exact
// ISO 8601 UTC strings this package sends on the wire — carried as strings (not time.Time)
// because the provider's own rejection is a STRING-shape rule (seconds present or not), and
// validating the string this package will actually SEND is more honest than validating a
// time.Time and re-deriving a string from it that could drift from what was checked.
type TimeRange struct {
	Start string
	End   string
}

func (t TimeRange) validate() error {
	if _, err := time.Parse(time.RFC3339, t.Start); err != nil {
		return fmt.Errorf("%w: start %q", ErrTimeRangeMissingSeconds, t.Start)
	}
	if _, err := time.Parse(time.RFC3339, t.End); err != nil {
		return fmt.Errorf("%w: end %q", ErrTimeRangeMissingSeconds, t.End)
	}
	return nil
}

// NewTimeRange builds a valid TimeRange from two time.Time instants, formatting each with
// time.RFC3339 (always seconds-inclusive) so a caller building a request from real clock
// values can never accidentally trip ErrTimeRangeMissingSeconds — that guard exists for a
// hand-built or imported string, not for this constructor's own output.
func NewTimeRange(start, end time.Time) TimeRange {
	return TimeRange{Start: start.UTC().Format(time.RFC3339), End: end.UTC().Format(time.RFC3339)}
}

// AnalyticsRequest is POST /api/v1/analytics/query's body, narrowed to what the Overview's
// KPI row needs (metrics + granularity + time_range, no dimension — 02-UI-SPEC.md's own
// data-source ledger: "same query, no dimension"). Filters/order_by/limit are documented
// but unused by this phase; adding them is a later phase's job, not a guess made here.
type AnalyticsRequest struct {
	Metrics     []string
	Granularity string
	TimeRange   TimeRange
}

type analyticsRequestWire struct {
	Metrics     []string `json:"metrics"`
	Granularity string   `json:"granularity,omitempty"`
	TimeRange   struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"time_range"`
}

// AnalyticsRow is one bucket's requested metrics, keyed by metric name. openapi.json types a
// row only as "an object with metric/dimension values", so each requested metric is read by
// its own name and nothing else in the row is guessed at.
type AnalyticsRow map[string]float64

// AnalyticsResponse is the decoded data.{data,metadata.row_count,warnings} envelope, rows
// oldest bucket first. Warnings are NOT an error: 02-OPENROUTER-API.md documents
// data.warnings as a normal part of a successful response, distinct from a non-2xx failure.
type AnalyticsResponse struct {
	Rows     []AnalyticsRow
	RowCount int
	Warnings []string
}

type analyticsResponseWire struct {
	Data struct {
		Data     []map[string]json.RawMessage `json:"data"`
		Metadata struct {
			RowCount int `json:"row_count"`
		} `json:"metadata"`
		Warnings []string `json:"warnings"`
	} `json:"data"`
}

// AnalyticsQuery calls POST /api/v1/analytics/query. Requires the MANAGEMENT credential
// (same credential table as ListKeys/GetCredits) — this is reconciliation tier only (D-08),
// never the number CRED-05's pre-flight refusal reads.
func AnalyticsQuery(ctx context.Context, client *http.Client, baseURL, apiKey string, req AnalyticsRequest) (AnalyticsResponse, error) {
	if len(req.Metrics) == 0 {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: no metrics requested")
	}
	if err := req.TimeRange.validate(); err != nil {
		return AnalyticsResponse{}, err
	}

	wireReq := analyticsRequestWire{Metrics: req.Metrics, Granularity: req.Granularity}
	wireReq.TimeRange.Start = req.TimeRange.Start
	wireReq.TimeRange.End = req.TimeRange.End
	payload, err := json.Marshal(wireReq)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/analytics/query", bytes.NewReader(payload))
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return AnalyticsResponse{}, classify(resp.StatusCode, respBody)
	}

	var wireResp analyticsResponseWire
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: decode response: %w", err)
	}
	rows, err := decodeRows(wireResp.Data.Data, req)
	if err != nil {
		return AnalyticsResponse{}, fmt.Errorf("openrouterprovision: analytics query: decode response: %w", err)
	}
	return AnalyticsResponse{
		Rows:     rows,
		RowCount: wireResp.Data.Metadata.RowCount,
		Warnings: wireResp.Data.Warnings,
	}, nil
}

// decodeRows reads each requested metric out of every row and returns the rows oldest bucket
// first. Measured live 2026-09-10 on a day-granularity query: rows come newest day first,
// each naming its bucket "date__day":"2026-09-09" (the date__<granularity> key the
// openapi.json example shows), and counts come quoted ("request_count":"43") while dollars
// and rates are bare numbers. The zero-padded bucket strings sort chronologically as text; a
// row without one keeps the provider's order.
func decodeRows(wire []map[string]json.RawMessage, req AnalyticsRequest) ([]AnalyticsRow, error) {
	type bucketRow struct {
		bucket string
		row    AnalyticsRow
	}
	decoded := make([]bucketRow, len(wire))
	for i, cells := range wire {
		if raw, ok := cells["date__"+req.Granularity]; ok {
			if err := json.Unmarshal(raw, &decoded[i].bucket); err != nil {
				return nil, fmt.Errorf("bucket date__%s: %w", req.Granularity, err)
			}
		}
		decoded[i].row = make(AnalyticsRow, len(req.Metrics))
		for _, metric := range req.Metrics {
			v, err := metricCell(cells[metric])
			if err != nil {
				return nil, fmt.Errorf("metric %s: %w", metric, err)
			}
			decoded[i].row[metric] = v
		}
	}
	sort.SliceStable(decoded, func(i, j int) bool { return decoded[i].bucket < decoded[j].bucket })
	rows := make([]AnalyticsRow, len(decoded))
	for i, d := range decoded {
		rows[i] = d.row
	}
	return rows, nil
}

// metricCell reads one metric value, bare or quoted: json.Number takes both and refuses a
// string that is not a number. A missing or null cell reads as zero, the per-bucket backstop
// spend_overview_api.go's kpiDTOsFrom documents.
func metricCell(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil || n == "" {
		return 0, err
	}
	return n.Float64()
}

// ErrUnsupportedRateMetric marks a metric AggregateRateMetricAcrossBuckets has no
// aggregation rule for. Only the two rate metrics the Overview's KPI row uses
// (cache_hit_rate, blended_cost_per_million_tokens) need one; every other KPI metric is
// naturally additive and is summed by headlineValue (below) instead.
var ErrUnsupportedRateMetric = errors.New("openrouterprovision: no rate-aggregation rule for this metric")

// AggregateRateMetricAcrossBuckets derives ONE headline value for a rate metric from a set
// of day-bucketed rows, instead of summing it: three days at a 90% cache-hit rate do not
// average to 270%, and blended $/1M summed across a week is a number with no unit.
//
//   - blended_cost_per_million_tokens is RE-DERIVED from the two ADDITIVE totals already in
//     the SAME KPI row's own metric set (total_usage, tokens_total): "cost per million
//     tokens" is definitionally cost/tokens*1e6, so Σtotal_usage / Σtokens_total * 1e6 is an
//     exact recomputation of the documented quantity, not a guess. The provider's own value
//     is never read: it came back 0 in every row, with and without a model dimension
//     (measured live 2026-09-10).
//   - cache_hit_rate has no such re-derivation available: 02-OPENROUTER-API.md names the
//     metric but not its numerator/denominator, and a SEPARATE possible_cache_hit_rate
//     metric exists in the same 38-metric list — implying more than one plausible
//     denominator. Guessing one would be exactly the "NEVER SUPPOSE" CLAUDE.md forbids for
//     an external API's behavior. It is instead a REQUEST-COUNT-WEIGHTED average: request_count
//     is already one of this row's own 5 fetched metrics, so a heavier-traffic day counts for
//     more than a quiet one — closer to the true aggregate than an unweighted mean, and
//     strictly more meaningful than a sum.
func AggregateRateMetricAcrossBuckets(rows []AnalyticsRow, metric string) (float64, error) {
	switch metric {
	case "blended_cost_per_million_tokens":
		var cost, tokens float64
		for _, r := range rows {
			cost += r["total_usage"]
			tokens += r["tokens_total"]
		}
		if tokens == 0 {
			return 0, nil
		}
		return cost / tokens * 1_000_000, nil
	case "cache_hit_rate":
		var weighted, weight float64
		for _, r := range rows {
			weighted += r["cache_hit_rate"] * r["request_count"]
			weight += r["request_count"]
		}
		if weight == 0 {
			return 0, nil
		}
		return weighted / weight, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedRateMetric, metric)
	}
}

// DeltaPercent computes the percent change from prior to current. A percent change against
// a ZERO prior-period value is mathematically undefined (division by zero) — this is the
// backstop 02-UI-SPEC.md's "Overview KPI row · overflow" row holds out explicitly. Decided
// here: return NO delta (nil) rather than inventing a number, the literal word "new", or an
// em-dash placeholder. The component renders a value THIS package already resolved instead
// of improvising a policy of its own — an em-dash or "new" is presentation, and either can
// be layered on a nil delta by the caller without this package having picked one for it.
func DeltaPercent(current, prior float64) *float64 {
	if prior == 0 {
		return nil
	}
	d := (current - prior) / prior * 100
	return &d
}

// additiveKPIMetrics sum correctly across day buckets (a dollar total, a request count, a
// token count); every other name in KPIMetrics is a rate and routes through
// AggregateRateMetricAcrossBuckets instead.
var additiveKPIMetrics = map[string]bool{
	"total_usage":   true,
	"request_count": true,
	"tokens_total":  true,
}

// KPIMetrics is the fixed 5-metric set the Overview's KPI row specifies
// (02-UI-SPEC.md §KPI row / §Data-source ledger): total spend, requests, token volume,
// cache hit rate, blended $/1M — one query, no dimension, day granularity.
var KPIMetrics = []string{
	"total_usage", "request_count", "tokens_total", "cache_hit_rate", "blended_cost_per_million_tokens",
}

// KPITile is one of the Overview's five stat tiles: the headline value for the CURRENT
// window, the percent delta against the PRIOR window (nil = no delta, DeltaPercent's own
// backstop), and the current window's day-bucketed series for the sparkline.
type KPITile struct {
	Metric string
	Value  float64
	Delta  *float64
	Series []float64
}

func headlineValue(rows []AnalyticsRow, metric string) (float64, error) {
	if additiveKPIMetrics[metric] {
		var sum float64
		for _, r := range rows {
			sum += r[metric]
		}
		return sum, nil
	}
	return AggregateRateMetricAcrossBuckets(rows, metric)
}

// KPIWindows fetches the Overview's five-tile KPI data over BOTH the current and prior
// window in two calls — 02-UI-SPEC.md's own data-source ledger accepts the doubled call
// count as an implementation cost, not a blocker — so the caller never has to orchestrate
// the pair itself. Each tile's Series is the CURRENT window's days, oldest first, each point
// the tile's own headline rule applied to that one day, so a point and the headline never
// disagree on what the metric means. A day with no OpenRouter traffic has no row, and so no
// point.
func KPIWindows(ctx context.Context, client *http.Client, baseURL, apiKey string, current, prior TimeRange) ([]KPITile, error) {
	curResp, err := AnalyticsQuery(ctx, client, baseURL, apiKey, AnalyticsRequest{
		Metrics: KPIMetrics, Granularity: "day", TimeRange: current,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouterprovision: kpi windows: current period: %w", err)
	}
	priorResp, err := AnalyticsQuery(ctx, client, baseURL, apiKey, AnalyticsRequest{
		Metrics: KPIMetrics, Granularity: "day", TimeRange: prior,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouterprovision: kpi windows: prior period: %w", err)
	}

	tiles := make([]KPITile, 0, len(KPIMetrics))
	for _, metric := range KPIMetrics {
		curVal, err := headlineValue(curResp.Rows, metric)
		if err != nil {
			return nil, fmt.Errorf("openrouterprovision: kpi windows: current %s: %w", metric, err)
		}
		priorVal, err := headlineValue(priorResp.Rows, metric)
		if err != nil {
			return nil, fmt.Errorf("openrouterprovision: kpi windows: prior %s: %w", metric, err)
		}
		series := make([]float64, 0, len(curResp.Rows))
		for _, row := range curResp.Rows {
			point, err := headlineValue([]AnalyticsRow{row}, metric)
			if err != nil {
				return nil, fmt.Errorf("openrouterprovision: kpi windows: current %s: %w", metric, err)
			}
			series = append(series, point)
		}
		tiles = append(tiles, KPITile{Metric: metric, Value: curVal, Delta: DeltaPercent(curVal, priorVal), Series: series})
	}
	return tiles, nil
}
