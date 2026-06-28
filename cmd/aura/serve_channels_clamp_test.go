package main

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestClampInt64ToInt is the go/incorrect-integer-conversion regression for the
// serve_channels todayCost path: the aggregate's int64 token sum (decoded via
// strconv.ParseInt) must narrow to int without truncation/sign-flip, saturating
// at the int range and flooring negatives to 0.
func TestClampInt64ToInt(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int
	}{
		{"zero", 0, 0},
		{"typical", 12345, 12345},
		{"negative_floors_to_zero", -1, 0},
		{"min_int64_floors_to_zero", math.MinInt64, 0},
		{"max_int_passes_through", math.MaxInt, math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampInt64ToInt(tc.in); got != tc.want {
				t.Fatalf("clampInt64ToInt(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestTelegramGetMeHTTPClientBoundsTimeoutAndContext(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	rt := &captureContextRoundTripper{}
	before := time.Now()
	client, cancelProbe := telegramGetMeHTTPClient(parent, rt)
	after := time.Now()
	defer cancelProbe()
	if client.Timeout != telegramGetMeTimeout {
		t.Fatalf("client timeout = %v, want %v", client.Timeout, telegramGetMeTimeout)
	}
	if err := doCapturedRequest(client); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !rt.hasDeadline {
		t.Fatal("probe request context has no deadline")
	}
	if rt.deadline.Before(before.Add(telegramGetMeTimeout)) || rt.deadline.After(after.Add(telegramGetMeTimeout)) {
		t.Fatalf("probe deadline = %v, want between %v and %v", rt.deadline, before.Add(telegramGetMeTimeout), after.Add(telegramGetMeTimeout))
	}

	earlyBefore := time.Now()
	early, cancelEarly := context.WithTimeout(context.Background(), 2*time.Second)
	earlyAfter := time.Now()
	defer cancelEarly()
	rt = &captureContextRoundTripper{}
	client, cancelProbe = telegramGetMeHTTPClient(early, rt)
	defer cancelProbe()
	if err := doCapturedRequest(client); err != nil {
		t.Fatalf("Do with early context: %v", err)
	}
	if rt.deadline.Before(earlyBefore.Add(2*time.Second)) || rt.deadline.After(earlyAfter.Add(2*time.Second)) {
		t.Fatalf("early parent deadline = %v, want between %v and %v", rt.deadline, earlyBefore.Add(2*time.Second), earlyAfter.Add(2*time.Second))
	}
}

type captureContextRoundTripper struct {
	hasDeadline bool
	deadline    time.Time
}

func (rt *captureContextRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.deadline, rt.hasDeadline = req.Context().Deadline()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func doCapturedRequest(client *http.Client) error {
	req, err := http.NewRequest(http.MethodGet, "http://telegram-probe.invalid/getMe", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
