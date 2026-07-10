package llm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- shared daemon-free test doubles (no network; every HTTP call is served by an
// injected http.RoundTripper returning a captured testdata fixture) ---

type fakeRoundTripper struct {
	mu    sync.Mutex
	calls int
	fn    func(*http.Request) (*http.Response, error)
}

func (f *fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.fn(r)
}

func (f *fakeRoundTripper) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func jsonResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return b
}

// newTestCapClient wires a ModelCapabilityClient onto an injected transport (and an
// optional injected clock) — same-package access to the unexported seams, mirroring
// the openai_compat "tests override directly" pattern.
func newTestCapClient(rt http.RoundTripper, clock *fakeClock, ttl time.Duration) *ModelCapabilityClient {
	c := NewModelCapabilityClient(Config{BaseURL: "https://openrouter.test/api/v1", APIKey: "k"}, ttl)
	c.httpClient = &http.Client{Transport: rt}
	if clock != nil {
		c.now = clock.now
	}
	return c
}

func effortsEqual(a, b []ReasoningEffort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsEffort(s []ReasoningEffort, e ReasoningEffort) bool {
	for _, v := range s {
		if v == e {
			return true
		}
	}
	return false
}

// --- Task 2 behavior ---

func TestParseModelReasoningCaps(t *testing.T) {
	fixture := readFixture(t, "testdata/openrouter_models.json")
	rt := &fakeRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("path = %s, want .../models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q, want Bearer k", got)
		}
		return jsonResponse(http.StatusOK, fixture), nil
	}}
	c := newTestCapClient(rt, nil, time.Hour)
	ctx := context.Background()

	// graduated model: the hostile "turbo" token is dropped by the allowlist clamp;
	// only the vocab tokens survive, order-preserved.
	rc, ok, err := c.ReasoningCapabilityFor(ctx, "deepseek/deepseek-v4-flash:nitro")
	if err != nil || !ok {
		t.Fatalf("graduated model: ok=%v err=%v", ok, err)
	}
	want := []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax}
	if !effortsEqual(rc.SupportedEfforts, want) {
		t.Errorf("SupportedEfforts = %v, want %v (turbo must be dropped)", rc.SupportedEfforts, want)
	}
	if rc.DefaultEffort != ReasoningEffortHigh {
		t.Errorf("DefaultEffort = %q, want high", rc.DefaultEffort)
	}
	if rc.Mandatory {
		t.Error("graduated model must not be mandatory")
	}
	if !rc.DefaultEnabled {
		t.Error("graduated model default_enabled should be true")
	}
	if len(rc.SupportedParams) == 0 {
		t.Error("SupportedParams should be parsed from supported_parameters")
	}

	// mandatory model: mandatory + default_effort parsed.
	rc2, ok2, err2 := c.ReasoningCapabilityFor(ctx, "openai/o4-mini")
	if err2 != nil || !ok2 {
		t.Fatalf("mandatory model: ok=%v err=%v", ok2, err2)
	}
	if !rc2.Mandatory {
		t.Error("o4-mini must be mandatory")
	}
	if rc2.DefaultEffort != ReasoningEffortMedium {
		t.Errorf("DefaultEffort = %q, want medium", rc2.DefaultEffort)
	}

	// no-reasoning model: present in the list but no reasoning object → empty efforts.
	rc3, ok3, err3 := c.ReasoningCapabilityFor(ctx, "meta-llama/llama-3.3-70b-instruct")
	if err3 != nil || !ok3 {
		t.Fatalf("no-reasoning model: ok=%v err=%v", ok3, err3)
	}
	if len(rc3.SupportedEfforts) != 0 {
		t.Errorf("no-reasoning model efforts = %v, want empty", rc3.SupportedEfforts)
	}
}

func TestModelCapabilityCacheTTL(t *testing.T) {
	fixture := readFixture(t, "testdata/openrouter_models.json")
	rt := &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, fixture), nil
	}}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c := newTestCapClient(rt, clock, time.Hour)
	ctx := context.Background()

	// cold fetch → exactly one HTTP call.
	if _, _, err := c.ReasoningCapabilityFor(ctx, "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatalf("cold fetch: %v", err)
	}
	if rt.callCount() != 1 {
		t.Fatalf("after cold fetch calls = %d, want 1", rt.callCount())
	}
	// warm hit (different model, same cached list) → NO 2nd HTTP call.
	if _, _, err := c.ReasoningCapabilityFor(ctx, "openai/o4-mini"); err != nil {
		t.Fatalf("warm hit: %v", err)
	}
	if rt.callCount() != 1 {
		t.Fatalf("after warm hit calls = %d, want 1 (cache must serve)", rt.callCount())
	}
	// advance the injected clock past TTL → re-fetch (2nd HTTP call).
	clock.advance(2 * time.Hour)
	if _, _, err := c.ReasoningCapabilityFor(ctx, "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatalf("post-expiry fetch: %v", err)
	}
	if rt.callCount() != 2 {
		t.Fatalf("after expiry calls = %d, want 2 (TTL must trigger re-fetch)", rt.callCount())
	}
}

func TestReasoningCapKey(t *testing.T) {
	// normalizeModelID strips the :nitro routing suffix so a suffixed active model
	// resolves to the base /models list key.
	if got := normalizeModelID("deepseek/deepseek-v4-flash:nitro"); got != "deepseek/deepseek-v4-flash" {
		t.Fatalf("normalizeModelID = %q, want deepseek/deepseek-v4-flash", got)
	}
	fixture := readFixture(t, "testdata/openrouter_models.json")
	rt := &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, fixture), nil
	}}
	c := newTestCapClient(rt, nil, time.Hour)
	// a :nitro-suffixed query must hit the base-id cache entry.
	rc, ok, err := c.ReasoningCapabilityFor(context.Background(), "deepseek/deepseek-v4-flash:nitro")
	if err != nil || !ok {
		t.Fatalf("suffixed lookup: ok=%v err=%v", ok, err)
	}
	if len(rc.SupportedEfforts) == 0 {
		t.Error("suffixed lookup should resolve to the base entry's efforts")
	}
}

func TestAllowedEfforts(t *testing.T) {
	fixture := readFixture(t, "testdata/openrouter_models.json")
	okRT := func() *fakeRoundTripper {
		return &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, fixture), nil
		}}
	}
	ctx := context.Background()

	// graduated model → detected, full clamped set, default from default_effort.
	src := &openRouterReasoningCaps{client: newTestCapClient(okRT(), nil, time.Hour), model: "deepseek/deepseek-v4-flash"}
	efforts, deflt, detected := src.AllowedEfforts(ctx)
	if !detected {
		t.Fatal("graduated model should be detected")
	}
	want := []ReasoningEffort{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax}
	if !effortsEqual(efforts, want) {
		t.Errorf("efforts = %v, want %v", efforts, want)
	}
	if deflt != ReasoningEffortHigh {
		t.Errorf("default = %q, want high", deflt)
	}

	// mandatory model → detected, "none" stripped (reasoning cannot be disabled).
	srcM := &openRouterReasoningCaps{client: newTestCapClient(okRT(), nil, time.Hour), model: "openai/o4-mini"}
	effM, _, detM := srcM.AllowedEfforts(ctx)
	if !detM {
		t.Fatal("mandatory model should be detected")
	}
	if containsEffort(effM, ReasoningEffortNone) {
		t.Errorf("mandatory model must not offer none/off, got %v", effM)
	}

	// no-reasoning model → not detected (safe floor upstream).
	srcN := &openRouterReasoningCaps{client: newTestCapClient(okRT(), nil, time.Hour), model: "meta-llama/llama-3.3-70b-instruct"}
	if _, _, det := srcN.AllowedEfforts(ctx); det {
		t.Error("no-reasoning model must yield detected=false")
	}

	// fetch error → detected=false, nil efforts.
	errRT := &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	}}
	srcErr := &openRouterReasoningCaps{client: newTestCapClient(errRT, nil, time.Hour), model: "deepseek/deepseek-v4-flash"}
	eff, dfl, det := srcErr.AllowedEfforts(ctx)
	if det || eff != nil || dfl != "" {
		t.Errorf("on fetch error want (nil,\"\",false); got (%v,%q,%v)", eff, dfl, det)
	}
}

func TestModelCapabilityDefensiveParse(t *testing.T) {
	ctx := context.Background()

	// malformed JSON body → error, no panic.
	badRT := &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []byte("{not valid json")), nil
	}}
	c := newTestCapClient(badRT, nil, time.Hour)
	if _, ok, err := c.ReasoningCapabilityFor(ctx, "x"); err == nil || ok {
		t.Errorf("malformed body: want err!=nil ok=false, got ok=%v err=%v", ok, err)
	}

	// non-2xx status → error.
	badStatus := &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, []byte("upstream boom")), nil
	}}
	c2 := newTestCapClient(badStatus, nil, time.Hour)
	if _, _, err := c2.ReasoningCapabilityFor(ctx, "x"); err == nil {
		t.Error("non-2xx: want err!=nil")
	}

	// valid-but-empty list → absent model, ok=false, no error.
	emptyRT := &fakeRoundTripper{fn: func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []byte(`{"data":[]}`)), nil
	}}
	c3 := newTestCapClient(emptyRT, nil, time.Hour)
	if _, ok, err := c3.ReasoningCapabilityFor(ctx, "absent/model"); ok || err != nil {
		t.Errorf("absent model: want ok=false err=nil, got ok=%v err=%v", ok, err)
	}
}
