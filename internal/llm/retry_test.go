package llm

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRetryBudget_Transient verifies the TRANSIENT bucket retries up to MaxRetries
// and that Request.Temperature is preserved across every retry attempt (D-08).
func TestRetryBudget_Transient(t *testing.T) {
	calls := 0
	capturedTemps := []*float64{}

	wantTemp := Float64Ptr(0.7)
	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			calls++
			capturedTemps = append(capturedTemps, req.Temperature)
			if calls < 5 {
				return Response{}, &APIError{StatusCode: 429}
			}
			return Response{Content: "ok"}, nil
		},
	}

	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   10 * time.Millisecond,
	})

	req := Request{
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: wantTemp,
	}
	resp, err := retry.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success after 5 attempts, got error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if calls != 5 {
		t.Errorf("calls = %d, want 5", calls)
	}
	// Verify temperature was preserved on every call (D-08).
	for i, temp := range capturedTemps {
		if temp == nil || *temp != *wantTemp {
			t.Errorf("call %d: temperature = %v, want %v", i+1, temp, wantTemp)
		}
	}
}

// TestRetryBudget_Content verifies the CONTENT bucket applies the temperature
// staircase on each retry and stops at MaxContentRetries.
func TestRetryBudget_Content(t *testing.T) {
	calls := 0
	capturedTemps := []*float64{}

	callerTemp := Float64Ptr(0.9) // caller's temp should NOT be used on CONTENT retries
	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			calls++
			capturedTemps = append(capturedTemps, req.Temperature)
			if calls < 3 {
				return Response{}, ErrSchemaValidation
			}
			return Response{Content: "fixed"}, nil
		},
	}

	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries:          5,
		BaseDelay:           1 * time.Millisecond,
		MaxDelay:            10 * time.Millisecond,
		MaxContentRetries:   3,
		ContentTemperatures: []float64{0.0, 0.3, 0.7},
		JitterRatio:         0.5,
	})

	req := Request{
		Messages:    []Message{{Role: "user", Content: "gen schema"}},
		Temperature: callerTemp,
	}
	resp, err := retry.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success on 3rd call, got error: %v", err)
	}
	if resp.Content != "fixed" {
		t.Errorf("Content = %q, want fixed", resp.Content)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}

	// First call uses caller's temperature (0.9).
	if capturedTemps[0] == nil || *capturedTemps[0] != 0.9 {
		t.Errorf("call 1 temperature = %v, want 0.9", capturedTemps[0])
	}
	// Second call (first CONTENT retry) → temperature staircase index 0 = 0.0.
	if capturedTemps[1] == nil || *capturedTemps[1] != 0.0 {
		t.Errorf("call 2 temperature = %v, want 0.0", capturedTemps[1])
	}
	// Third call (second CONTENT retry) → temperature staircase index 1 = 0.3.
	if capturedTemps[2] == nil || *capturedTemps[2] != 0.3 {
		t.Errorf("call 3 temperature = %v, want 0.3", capturedTemps[2])
	}
}

// TestRetry_NudgeAppended verifies that on a CONTENT retry the validation nudge
// is appended to req.Messages as a "system" role message.
func TestRetry_NudgeAppended(t *testing.T) {
	type callRecord struct {
		msgCount int
		lastRole string
		lastBody string
	}
	records := []callRecord{}

	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			last := req.Messages[len(req.Messages)-1]
			records = append(records, callRecord{
				msgCount: len(req.Messages),
				lastRole: last.Role,
				lastBody: last.Content,
			})
			if len(records) == 1 {
				return Response{}, ErrSchemaValidation
			}
			return Response{Content: "ok"}, nil
		},
	}

	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries:          5,
		BaseDelay:           1 * time.Millisecond,
		MaxDelay:            10 * time.Millisecond,
		MaxContentRetries:   3,
		ContentTemperatures: []float64{0.0, 0.3, 0.7},
		JitterRatio:         0.5,
	})

	_, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "gen"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(records))
	}
	if records[1].msgCount != records[0].msgCount+1 {
		t.Errorf("2nd call msgCount = %d, want %d (first + nudge)", records[1].msgCount, records[0].msgCount+1)
	}
	if records[1].lastRole != "system" {
		t.Errorf("nudge role = %q, want system", records[1].lastRole)
	}
	if !strings.HasPrefix(records[1].lastBody, "Previous output failed validation:") {
		t.Errorf("nudge content = %q, want prefix 'Previous output failed validation:'", records[1].lastBody)
	}
}

// TestJitterDistribution verifies jitteredBackoff produces samples within the
// expected range and that the mean is close to the theoretical value.
func TestJitterDistribution(t *testing.T) {
	const N = 1000
	base := 1 * time.Second
	max := 30 * time.Second
	ratio := 0.5

	var total time.Duration
	for i := 0; i < N; i++ {
		d := jitteredBackoff(0, base, max, ratio)
		if d < 0 {
			t.Errorf("sample %d: negative duration %v", i, d)
		}
		if d > max {
			t.Errorf("sample %d: duration %v exceeds max %v", i, d, max)
		}
		total += d
	}

	mean := total / N
	// Theory: base*2^0 = 1s, jitter ±50% → range [0.5s, 1.5s], mean ~1s.
	// Allow ±15% tolerance.
	lo := 850 * time.Millisecond
	hi := 1150 * time.Millisecond
	if mean < lo || mean > hi {
		t.Errorf("mean = %v, want in [%v, %v]", mean, lo, hi)
	}
}

// TestRetry_PermanentNoRetry verifies that PERMANENT errors cause immediate
// return with exactly 1 call (no retry).
func TestRetry_PermanentNoRetry(t *testing.T) {
	calls := 0
	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			calls++
			return Response{}, &APIError{StatusCode: 401}
		},
	}

	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   10 * time.Millisecond,
	})

	_, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from permanent failure")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (PERMANENT should not retry)", calls)
	}
}

// TestRetry_BackwardsCompat_DefaultConfig verifies NewRetryClient works with
// DefaultRetryConfig and that the content temperatures are populated.
func TestRetry_BackwardsCompat_DefaultConfig(t *testing.T) {
	mock := &mockClient{}
	rc := NewRetryClient(mock, DefaultRetryConfig())
	if rc == nil {
		t.Fatal("NewRetryClient returned nil")
	}
	if len(rc.contentTemperatures) == 0 {
		t.Error("contentTemperatures should be populated by DefaultRetryConfig")
	}
	if rc.contentTemperatures[0] != 0.0 || rc.contentTemperatures[1] != 0.3 || rc.contentTemperatures[2] != 0.7 {
		t.Errorf("contentTemperatures = %v, want [0.0, 0.3, 0.7]", rc.contentTemperatures)
	}
	if rc.maxContentRetries != 3 {
		t.Errorf("maxContentRetries = %d, want 3", rc.maxContentRetries)
	}
	if rc.jitterRatio != 0.5 {
		t.Errorf("jitterRatio = %v, want 0.5", rc.jitterRatio)
	}
}
