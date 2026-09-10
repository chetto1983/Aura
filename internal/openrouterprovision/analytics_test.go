package openrouterprovision_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// keysListPayload is GET /api/v1/keys's response shape transcribed from
// 02-OPENROUTER-API.md (retrieved 2026-09-08): the same keyDataWire shape GET/POST/PATCH
// share, as an array under "data". One row carries a null limit (uncapped); the other
// carries a real, non-null limit_remaining of zero (a real zero balance, not absence).
const keysListPayload = `{"data":[
  {"hash":"hash-1","label":"sk-or-v1-aaa...111","name":"identity-1","disabled":false,"created_at":"2026-09-08T00:00:00Z","updated_at":"2026-09-08T00:00:00Z","expires_at":null,"limit":null,"limit_remaining":null,"limit_reset":"monthly","usage":1.5,"usage_daily":0,"usage_weekly":0,"usage_monthly":0,"byok_usage":0,"byok_usage_daily":0,"byok_usage_weekly":0,"byok_usage_monthly":0,"external_user":"identity-1","include_byok_in_limit":false,"creator_user_id":null,"workspace_id":"ws-1"},
  {"hash":"hash-2","label":"sk-or-v1-bbb...222","name":"identity-2","disabled":false,"created_at":"2026-09-08T00:00:00Z","updated_at":"2026-09-08T00:00:00Z","expires_at":null,"limit":5,"limit_remaining":0,"limit_reset":"monthly","usage":5,"usage_daily":0,"usage_weekly":0,"usage_monthly":0,"byok_usage":0,"byok_usage_daily":0,"byok_usage_weekly":0,"byok_usage_monthly":0,"external_user":"identity-2","include_byok_in_limit":false,"creator_user_id":null,"workspace_id":"ws-1"}
]}`

func keysServer(t *testing.T, capturedAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/keys" {
			http.NotFound(w, r)
			return
		}
		if capturedAuth != nil {
			*capturedAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keysListPayload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestListKeysDecodesRoster proves the whole roster decodes in one call: hash/label/
// external_user/usage all present, and a null limit decodes as ABSENT (nil) rather than a
// zero — the same absence-vs-data discipline client_test.go already pins for
// limit_remaining, extended here to a second row whose limit_remaining is a real,
// non-null zero.
func TestListKeysDecodesRoster(t *testing.T) {
	srv := keysServer(t, nil)
	records, err := openrouterprovision.ListKeys(context.Background(), srv.Client(), srv.URL, "sk-mgmt")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	first := records[0]
	if first.Hash != "hash-1" || first.Label != "sk-or-v1-aaa...111" || first.ExternalUser != "identity-1" {
		t.Errorf("first record = %+v, want hash-1/sk-or-v1-aaa...111/identity-1", first)
	}
	if first.Usage != 1.5 {
		t.Errorf("first.Usage = %v, want 1.5", first.Usage)
	}
	if first.Limit != nil {
		t.Errorf("first.Limit = %v, want nil (null on the wire == uncapped, not zero)", *first.Limit)
	}
	if first.LimitRemaining != nil {
		t.Errorf("first.LimitRemaining = %v, want nil (null == absent)", *first.LimitRemaining)
	}

	second := records[1]
	if second.Limit == nil || second.Limit.String() != "5.00" {
		t.Errorf("second.Limit = %v, want 5.00", second.Limit)
	}
	if second.LimitRemaining == nil || second.LimitRemaining.String() != "0.00" {
		t.Errorf("second.LimitRemaining = %v, want a real, non-nil zero", second.LimitRemaining)
	}
}

// TestListKeysSendsManagementAuthorization proves the outgoing request carries the
// management credential — the same convention every other verb in this package follows.
func TestListKeysSendsManagementAuthorization(t *testing.T) {
	var auth string
	srv := keysServer(t, &auth)
	if _, err := openrouterprovision.ListKeys(context.Background(), srv.Client(), srv.URL, "sk-mgmt-secret"); err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if auth != "Bearer sk-mgmt-secret" {
		t.Errorf("Authorization = %q, want Bearer sk-mgmt-secret", auth)
	}
}

func TestListKeysProviderErrorClassifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"revoked"}}`))
	}))
	defer srv.Close()
	if _, err := openrouterprovision.ListKeys(context.Background(), srv.Client(), srv.URL, "sk-mgmt"); !errors.Is(err, openrouterprovision.ErrKeyRevoked) {
		t.Fatalf("err = %v, want ErrKeyRevoked", err)
	}
}

func TestListKeysDecodeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if _, err := openrouterprovision.ListKeys(context.Background(), srv.Client(), srv.URL, "sk-mgmt"); err == nil {
		t.Fatal("ListKeys: want a decode error, got nil")
	}
}

// TestGetCreditsDecodesPool proves total_credits/total_usage decode; remaining is left to
// the caller to compute (M-12: the over-allocation banner needs it alongside a separately
// summed Σ(cap), which this package cannot know about).
func TestGetCreditsDecodesPool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/credits" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"total_credits":90,"total_usage":74.078437781}}`))
	}))
	defer srv.Close()

	credits, err := openrouterprovision.GetCredits(context.Background(), srv.Client(), srv.URL, "sk-mgmt")
	if err != nil {
		t.Fatalf("GetCredits: %v", err)
	}
	if credits.TotalCredits != 90 {
		t.Errorf("TotalCredits = %v, want 90", credits.TotalCredits)
	}
	if credits.TotalUsage != 74.078437781 {
		t.Errorf("TotalUsage = %v, want 74.078437781", credits.TotalUsage)
	}
}

func TestGetCreditsProviderErrorClassifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	defer srv.Close()
	if _, err := openrouterprovision.GetCredits(context.Background(), srv.Client(), srv.URL, "sk-mgmt"); !errors.Is(err, openrouterprovision.ErrKeyNotFound) {
		t.Fatalf("err = %v, want ErrKeyNotFound", err)
	}
}

// TestAnalyticsQueryRequiresSecondsInTimeRange proves a minute-precision time_range is
// refused LOCALLY, before any request goes out — the API rejects minute precision
// (02-OPENROUTER-API.md), and a local guard is faster to diagnose than a provider 400.
func TestAnalyticsQueryRequiresSecondsInTimeRange(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"data":[],"metadata":{"row_count":0},"warnings":[]}}`))
	}))
	defer srv.Close()

	req := openrouterprovision.AnalyticsRequest{
		Metrics:     []string{"total_usage"},
		Granularity: "day",
		// Minute precision — no seconds group. The provider rejects this; this package
		// must never even try.
		TimeRange: openrouterprovision.TimeRange{Start: "2026-09-08T10:00Z", End: "2026-09-08T11:00Z"},
	}
	_, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req)
	if !errors.Is(err, openrouterprovision.ErrTimeRangeMissingSeconds) {
		t.Fatalf("err = %v, want ErrTimeRangeMissingSeconds", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 — a bad time_range must never reach the provider", requests)
	}
}

func TestNewTimeRangeAlwaysValid(t *testing.T) {
	start, err := time.Parse(time.RFC3339, "2026-09-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	end, err := time.Parse(time.RFC3339, "2026-09-08T00:00:00Z")
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}
	tr := openrouterprovision.NewTimeRange(start, end)
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"data":[],"metadata":{"row_count":0},"warnings":[]}}`))
	}))
	defer srv.Close()
	req := openrouterprovision.AnalyticsRequest{Metrics: []string{"total_usage"}, Granularity: "day", TimeRange: tr}
	if _, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
		t.Fatalf("AnalyticsQuery: %v", err)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 — NewTimeRange's own output must always validate", requests)
	}
}

// TestAnalyticsQuerySendsMetricsAndGranularity proves the captured body carries the
// requested metric names and the granularity, verbatim.
func TestAnalyticsQuerySendsMetricsAndGranularity(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/analytics/query" {
			http.NotFound(w, r)
			return
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		body = string(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"data":[],"metadata":{"row_count":0},"warnings":[]}}`))
	}))
	defer srv.Close()

	req := openrouterprovision.AnalyticsRequest{
		Metrics:     openrouterprovision.KPIMetrics,
		Granularity: "day",
		TimeRange:   openrouterprovision.TimeRange{Start: "2026-09-01T00:00:00Z", End: "2026-09-08T00:00:00Z"},
	}
	if _, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
		t.Fatalf("AnalyticsQuery: %v", err)
	}
	for _, metric := range openrouterprovision.KPIMetrics {
		if !strings.Contains(body, `"`+metric+`"`) {
			t.Errorf("captured body = %s, want it to contain metric %q", body, metric)
		}
	}
	if !strings.Contains(body, `"granularity":"day"`) {
		t.Errorf("captured body = %s, want granularity day", body)
	}
}

// TestAnalyticsQueryDecodesEnvelope proves data.data rows, data.metadata.row_count and
// data.warnings all decode, and a response CARRYING warnings is not treated as a failure.
func TestAnalyticsQueryDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"data":[{"total_usage":1.5,"request_count":10},{"total_usage":2.5,"request_count":20}],"metadata":{"row_count":2,"query_time_ms":12,"truncated":false},"warnings":["partial data for one day"],"cachedAt":1789044730539}}`))
	}))
	defer srv.Close()

	req := openrouterprovision.AnalyticsRequest{
		Metrics:     []string{"total_usage", "request_count"},
		Granularity: "day",
		TimeRange:   openrouterprovision.TimeRange{Start: "2026-09-01T00:00:00Z", End: "2026-09-08T00:00:00Z"},
	}
	resp, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req)
	if err != nil {
		t.Fatalf("AnalyticsQuery: %v (warnings must not be treated as failure)", err)
	}
	if resp.RowCount != 2 {
		t.Errorf("RowCount = %d, want 2", resp.RowCount)
	}
	if len(resp.Rows) != 2 || resp.Rows[0]["total_usage"] != 1.5 || resp.Rows[1]["request_count"] != 20 {
		t.Errorf("Rows = %+v, want two rows carrying the requested metrics", resp.Rows)
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != "partial data for one day" {
		t.Errorf("Warnings = %v, want one warning preserved", resp.Warnings)
	}
}

func TestAnalyticsQueryNoMetricsRefused(t *testing.T) {
	req := openrouterprovision.AnalyticsRequest{
		Granularity: "day",
		TimeRange:   openrouterprovision.TimeRange{Start: "2026-09-01T00:00:00Z", End: "2026-09-08T00:00:00Z"},
	}
	if _, err := openrouterprovision.AnalyticsQuery(context.Background(), http.DefaultClient, "http://unused", "sk-mgmt", req); err == nil {
		t.Fatal("want an error for zero metrics, got nil")
	}
}

func TestAnalyticsQueryProviderErrorClassifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Key limit exceeded (monthly limit)"}}`))
	}))
	defer srv.Close()
	req := openrouterprovision.AnalyticsRequest{
		Metrics:   []string{"total_usage"},
		TimeRange: openrouterprovision.TimeRange{Start: "2026-09-01T00:00:00Z", End: "2026-09-08T00:00:00Z"},
	}
	if _, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); !errors.Is(err, openrouterprovision.ErrKeyLimitExceeded) {
		t.Fatalf("err = %v, want ErrKeyLimitExceeded", err)
	}
}

func TestAnalyticsQueryDecodeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	req := openrouterprovision.AnalyticsRequest{
		Metrics:   []string{"total_usage"},
		TimeRange: openrouterprovision.TimeRange{Start: "2026-09-01T00:00:00Z", End: "2026-09-08T00:00:00Z"},
	}
	if _, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err == nil {
		t.Fatal("want a decode error, got nil")
	}
}

func serveAnalytics(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dayQuery(metrics ...string) openrouterprovision.AnalyticsRequest {
	return openrouterprovision.AnalyticsRequest{
		Metrics:     metrics,
		Granularity: "day",
		TimeRange:   openrouterprovision.TimeRange{Start: "2026-08-29T00:00:00Z", End: "2026-09-10T11:32:10Z"},
	}
}

// liveKPIPayload is the live API's answer to the KPI query, captured verbatim on 2026-09-10:
// newest day first, idle days absent, counts quoted, cachedAt an epoch-milliseconds number,
// and blended cost 0 in every row.
const liveKPIPayload = `{"data":{"data":[{"date__day":"2026-09-09","total_usage":0.005235,"request_count":"4","tokens_total":"66342","cache_hit_rate":0.19096112993983816,"blended_cost_per_million_tokens":0},{"date__day":"2026-09-08","total_usage":0.074694,"request_count":"43","tokens_total":"1152597","cache_hit_rate":0.7594291386610319,"blended_cost_per_million_tokens":0},{"date__day":"2026-09-07","total_usage":0.013649,"request_count":"10","tokens_total":"199878","cache_hit_rate":0.11119815205383148,"blended_cost_per_million_tokens":0},{"date__day":"2026-09-04","total_usage":0.086801,"request_count":"80","tokens_total":"1052695","cache_hit_rate":0.5022285356076858,"blended_cost_per_million_tokens":0},{"date__day":"2026-09-03","total_usage":0.088529,"request_count":"59","tokens_total":"1162846","cache_hit_rate":0.24654000844388152,"blended_cost_per_million_tokens":0},{"date__day":"2026-08-29","total_usage":2.505722,"request_count":"677","tokens_total":"17251624","cache_hit_rate":0.6224484147034853,"blended_cost_per_million_tokens":0}],"metadata":{"query_time_ms":15,"row_count":6,"truncated":false},"cachedAt":1789044730539}}`

// TestAnalyticsQueryDecodesTheLiveKPIResponse replays the live answer: the quoted counts
// decode, the date bucket is not taken for a metric, and the rows come back oldest day first.
func TestAnalyticsQueryDecodesTheLiveKPIResponse(t *testing.T) {
	srv := serveAnalytics(t, liveKPIPayload)
	resp, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", dayQuery(openrouterprovision.KPIMetrics...))
	if err != nil {
		t.Fatalf("AnalyticsQuery: %v", err)
	}
	if len(resp.Rows) != 6 || resp.RowCount != 6 {
		t.Fatalf("len(Rows) = %d, RowCount = %d, want 6 and 6", len(resp.Rows), resp.RowCount)
	}
	oldest, newest := resp.Rows[0], resp.Rows[5]
	if oldest["request_count"] != 677 || oldest["tokens_total"] != 17251624 || oldest["total_usage"] != 2.505722 {
		t.Errorf("Rows[0] = %v, want 2026-08-29: 677 requests, 17251624 tokens, $2.505722", oldest)
	}
	if newest["request_count"] != 4 || newest["cache_hit_rate"] != 0.19096112993983816 {
		t.Errorf("Rows[5] = %v, want 2026-09-09: 4 requests at a 0.19096112993983816 cache hit rate", newest)
	}
	if len(oldest) != len(openrouterprovision.KPIMetrics) {
		t.Errorf("Rows[0] = %v, want exactly the %d requested metrics", oldest, len(openrouterprovision.KPIMetrics))
	}
}

func TestAnalyticsQueryReadsANullMetricAsZero(t *testing.T) {
	srv := serveAnalytics(t, `{"data":{"data":[{"date__day":"2026-09-09","request_count":"4","cache_hit_rate":null}],"metadata":{"row_count":1}}}`)
	resp, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", dayQuery("request_count", "cache_hit_rate"))
	if err != nil {
		t.Fatalf("AnalyticsQuery: %v", err)
	}
	if row := resp.Rows[0]; row["request_count"] != 4 || row["cache_hit_rate"] != 0 {
		t.Errorf("Rows[0] = %v, want 4 requests and a zero cache hit rate", row)
	}
}

// A cell that is neither a number nor a quoted number fails the decode by name, instead of
// reading as zero and passing for a quiet day.
func TestAnalyticsQueryRefusesACellItCannotRead(t *testing.T) {
	for name, tc := range map[string]struct{ row, want string }{
		"metric that is not a number": {`{"date__day":"2026-09-09","request_count":"many"}`, "metric request_count"},
		"bucket that is not a string": {`{"date__day":20260909,"request_count":"4"}`, "bucket date__day"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := serveAnalytics(t, `{"data":{"data":[`+tc.row+`],"metadata":{"row_count":1}}}`)
			_, err := openrouterprovision.AnalyticsQuery(context.Background(), srv.Client(), srv.URL, "sk-mgmt", dayQuery("request_count"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a decode error naming %q", err, tc.want)
			}
		})
	}
}

// TestKPIWindowsSeriesRunsOldestFirstAndDerivesBlendedPerDay feeds the live shape (newest
// day first, blended cost 0 in every row) and pins every sparkline in calendar order, the
// blended point re-derived from that day's own spend and tokens.
func TestKPIWindowsSeriesRunsOldestFirstAndDerivesBlendedPerDay(t *testing.T) {
	srv := serveAnalytics(t, `{"data":{"data":[
		{"date__day":"2026-09-09","total_usage":1.0,"request_count":"4","tokens_total":"250000","cache_hit_rate":0.25,"blended_cost_per_million_tokens":0},
		{"date__day":"2026-09-08","total_usage":3.0,"request_count":"40","tokens_total":"1500000","cache_hit_rate":0.75,"blended_cost_per_million_tokens":0}
	],"metadata":{"row_count":2},"cachedAt":1789044730539}}`)
	current := openrouterprovision.TimeRange{Start: "2026-08-30T00:00:00Z", End: "2026-09-10T09:42:07Z"}
	prior := openrouterprovision.TimeRange{Start: "2026-08-18T00:00:00Z", End: "2026-08-30T00:00:00Z"}
	tiles, err := openrouterprovision.KPIWindows(context.Background(), srv.Client(), srv.URL, "sk-mgmt", current, prior)
	if err != nil {
		t.Fatalf("KPIWindows: %v", err)
	}
	want := map[string][]float64{
		"total_usage":    {3.0, 1.0},
		"request_count":  {40, 4},
		"tokens_total":   {1_500_000, 250_000},
		"cache_hit_rate": {0.75, 0.25},
		// Hand-computed: 3.0/1,500,000*1e6 = 2.0 on 09-08, 1.0/250,000*1e6 = 4.0 on 09-09.
		"blended_cost_per_million_tokens": {2.0, 4.0},
	}
	for _, tile := range tiles {
		if !slices.Equal(tile.Series, want[tile.Metric]) {
			t.Errorf("%s.Series = %v, want %v", tile.Metric, tile.Series, want[tile.Metric])
		}
	}
}

// TestAggregateRateMetricAcrossBuckets pins the two decided aggregation rules against
// HAND-COMPUTED expected values, never against the function's own output.
func TestAggregateRateMetricAcrossBuckets(t *testing.T) {
	t.Run("blended_cost_per_million_tokens re-derives from totals", func(t *testing.T) {
		rows := []openrouterprovision.AnalyticsRow{
			{"total_usage": 1.0, "tokens_total": 500_000},
			{"total_usage": 3.0, "tokens_total": 1_500_000},
		}
		// Hand-computed: Σcost=4.0, Σtokens=2,000,000 → 4.0/2,000,000*1e6 = 2.0.
		// A naive per-day average ((1/500000+3/1500000)/2 * 1e6 = 3.0) would be WRONG —
		// this assertion is what catches a regression back to that shape.
		got, err := openrouterprovision.AggregateRateMetricAcrossBuckets(rows, "blended_cost_per_million_tokens")
		if err != nil {
			t.Fatalf("AggregateRateMetricAcrossBuckets: %v", err)
		}
		if got != 2.0 {
			t.Errorf("got %v, want 2.0 (hand-computed: Σ1+3=4 / Σ500000+1500000=2000000 * 1e6)", got)
		}
	})

	t.Run("cache_hit_rate is request-count-weighted, not a naive average", func(t *testing.T) {
		rows := []openrouterprovision.AnalyticsRow{
			{"cache_hit_rate": 0.90, "request_count": 100},
			{"cache_hit_rate": 0.10, "request_count": 900},
		}
		// Hand-computed weighted average: (0.90*100 + 0.10*900) / (100+900)
		//   = (90 + 90) / 1000 = 0.18.
		// A naive UNWEIGHTED average would give (0.90+0.10)/2 = 0.50 — very different,
		// and wrong in the direction that hides the heavy-traffic day's low hit rate.
		got, err := openrouterprovision.AggregateRateMetricAcrossBuckets(rows, "cache_hit_rate")
		if err != nil {
			t.Fatalf("AggregateRateMetricAcrossBuckets: %v", err)
		}
		if got != 0.18 {
			t.Errorf("got %v, want 0.18 (request-count-weighted, not the naive 0.50 average)", got)
		}
	})

	t.Run("zero denominators degrade to zero, not NaN or a panic", func(t *testing.T) {
		if got, err := openrouterprovision.AggregateRateMetricAcrossBuckets(nil, "blended_cost_per_million_tokens"); err != nil || got != 0 {
			t.Errorf("blended over no rows = (%v, %v), want (0, nil)", got, err)
		}
		if got, err := openrouterprovision.AggregateRateMetricAcrossBuckets(nil, "cache_hit_rate"); err != nil || got != 0 {
			t.Errorf("cache_hit_rate over no rows = (%v, %v), want (0, nil)", got, err)
		}
	})

	t.Run("an unsupported metric is refused rather than silently summed", func(t *testing.T) {
		_, err := openrouterprovision.AggregateRateMetricAcrossBuckets(
			[]openrouterprovision.AnalyticsRow{{"total_usage": 1}}, "total_usage",
		)
		if !errors.Is(err, openrouterprovision.ErrUnsupportedRateMetric) {
			t.Fatalf("err = %v, want ErrUnsupportedRateMetric (total_usage is additive, not a rate — the caller sums it, this function must refuse it)", err)
		}
	})
}

// TestDeltaWhenPriorPeriodIsZero pins the decided behaviour for an undefined percent
// change: NO delta (nil), never a fabricated number.
func TestDeltaWhenPriorPeriodIsZero(t *testing.T) {
	if got := openrouterprovision.DeltaPercent(5.0, 0); got != nil {
		t.Errorf("DeltaPercent(5, 0) = %v, want nil (undefined percent change against a zero prior period)", *got)
	}
	if got := openrouterprovision.DeltaPercent(0, 0); got != nil {
		t.Errorf("DeltaPercent(0, 0) = %v, want nil", *got)
	}
	// Hand-computed non-degenerate case: (150-100)/100*100 = 50.
	got := openrouterprovision.DeltaPercent(150, 100)
	if got == nil || *got != 50 {
		t.Fatalf("DeltaPercent(150, 100) = %v, want 50", got)
	}
	// A decrease is a negative delta, not clamped or absolute-valued.
	down := openrouterprovision.DeltaPercent(50, 100)
	if down == nil || *down != -50 {
		t.Fatalf("DeltaPercent(50, 100) = %v, want -50", down)
	}
}

func TestKPIWindowsComposesBothCallsAndAppliesAggregationRules(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call++
		w.WriteHeader(http.StatusOK)
		if call == 1 {
			// Current window: two day-buckets.
			_, _ = w.Write([]byte(`{"data":{"data":[
				{"total_usage":1.0,"request_count":10,"tokens_total":100000,"cache_hit_rate":0.5,"blended_cost_per_million_tokens":0},
				{"total_usage":3.0,"request_count":30,"tokens_total":300000,"cache_hit_rate":0.9,"blended_cost_per_million_tokens":0}
			],"metadata":{"row_count":2},"warnings":[]}}`))
			return
		}
		// Prior window: one day-bucket, smaller totals.
		_, _ = w.Write([]byte(`{"data":{"data":[
			{"total_usage":2.0,"request_count":20,"tokens_total":200000,"cache_hit_rate":0.4,"blended_cost_per_million_tokens":0}
		],"metadata":{"row_count":1},"warnings":[]}}`))
	}))
	defer srv.Close()

	current := openrouterprovision.TimeRange{Start: "2026-09-07T00:00:00Z", End: "2026-09-08T00:00:00Z"}
	prior := openrouterprovision.TimeRange{Start: "2026-09-06T00:00:00Z", End: "2026-09-07T00:00:00Z"}
	tiles, err := openrouterprovision.KPIWindows(context.Background(), srv.Client(), srv.URL, "sk-mgmt", current, prior)
	if err != nil {
		t.Fatalf("KPIWindows: %v", err)
	}
	if call != 2 {
		t.Fatalf("provider calls = %d, want 2 (current + prior)", call)
	}
	if len(tiles) != len(openrouterprovision.KPIMetrics) {
		t.Fatalf("len(tiles) = %d, want %d", len(tiles), len(openrouterprovision.KPIMetrics))
	}

	byMetric := make(map[string]openrouterprovision.KPITile, len(tiles))
	for _, tile := range tiles {
		byMetric[tile.Metric] = tile
	}

	spend := byMetric["total_usage"]
	if spend.Value != 4.0 { // 1.0 + 3.0, summed (additive metric)
		t.Errorf("total_usage.Value = %v, want 4.0", spend.Value)
	}
	if spend.Delta == nil || *spend.Delta != 100 { // (4-2)/2*100
		t.Errorf("total_usage.Delta = %v, want 100", spend.Delta)
	}
	if len(spend.Series) != 2 || spend.Series[0] != 1.0 || spend.Series[1] != 3.0 {
		t.Errorf("total_usage.Series = %v, want [1.0, 3.0]", spend.Series)
	}

	cacheHit := byMetric["cache_hit_rate"]
	// Weighted: (0.5*10 + 0.9*30) / 40 = (5+27)/40 = 0.8
	if cacheHit.Value != 0.8 {
		t.Errorf("cache_hit_rate.Value = %v, want 0.8 (request-count-weighted)", cacheHit.Value)
	}
}
