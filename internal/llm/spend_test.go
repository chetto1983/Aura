package llm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The shape verified against the live endpoint on 2026-07-29. Fields Aura does not read
// (is_management_key, limit, byok_*, rate_limit) are present on purpose: the decoder must
// ignore them rather than fail.
const keyPayload = `{"data":{"label":"sk-or-v1-0c3...ec6","is_management_key":false,
  "limit":null,"limit_remaining":null,"usage":2.722773972,"usage_daily":0,
  "usage_weekly":0.015453927,"usage_monthly":0.723534697,"byok_usage":0,
  "is_free_tier":false,"rate_limit":{"requests":-1,"interval":"10s"}}}`

func keyServer(t *testing.T, seenAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/key" {
			http.NotFound(w, r)
			return
		}
		if seenAuth != nil {
			*seenAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keyPayload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchSpendReadsTheFourWindows(t *testing.T) {
	var seen string
	srv := keyServer(t, &seen)

	got, err := llm.FetchSpend(context.Background(), srv.Client(), srv.URL, "sk-test")
	if err != nil {
		t.Fatalf("FetchSpend: %v", err)
	}
	want := llm.Spend{Total: 2.722773972, Daily: 0, Weekly: 0.015453927, Monthly: 0.723534697}
	if got != want {
		t.Errorf("spend = %+v, want %+v", got, want)
	}
	if seen != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want the inference key — /key needs no management key", seen)
	}
}

// A zero daily spend is DATA, not an absence: it means no turn has been billed yet
// today. It must be returned as 0 with a nil error so the caller can render "$0.00"
// rather than "unavailable".
func TestFetchSpendTreatsZeroDailyAsAValue(t *testing.T) {
	srv := keyServer(t, nil)
	got, err := llm.FetchSpend(context.Background(), srv.Client(), srv.URL, "k")
	if err != nil {
		t.Fatalf("FetchSpend: %v", err)
	}
	if got.Daily != 0 || got.Monthly == 0 {
		t.Errorf("spend = %+v: want Daily==0 carried through and Monthly populated", got)
	}
}

func TestFetchSpendUnavailableCases(t *testing.T) {
	srv := keyServer(t, nil)

	t.Run("no_key_configured", func(t *testing.T) {
		_, err := llm.FetchSpend(context.Background(), srv.Client(), srv.URL, "  ")
		if !errors.Is(err, llm.ErrSpendUnavailable) {
			t.Errorf("err = %v, want ErrSpendUnavailable", err)
		}
	})

	t.Run("non_200", func(t *testing.T) {
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer down.Close()
		_, err := llm.FetchSpend(context.Background(), down.Client(), down.URL, "k")
		if !errors.Is(err, llm.ErrSpendUnavailable) {
			t.Errorf("err = %v, want ErrSpendUnavailable", err)
		}
	})

	t.Run("malformed_body", func(t *testing.T) {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
		defer bad.Close()
		_, err := llm.FetchSpend(context.Background(), bad.Client(), bad.URL, "k")
		if !errors.Is(err, llm.ErrSpendUnavailable) {
			t.Errorf("err = %v, want ErrSpendUnavailable", err)
		}
	})
}

// A local backend has no account behind it. The sentinel is distinct from Unavailable so
// a caller can stay silent instead of reporting a failure that is not one.
func TestConfigSpendSkipsLocalBackend(t *testing.T) {
	cfg := llm.Config{
		Provider: "llamacpp",
		BaseURL:  "http://127.0.0.1:1/v1", // a dial here would fail loudly
		APIKey:   "irrelevant",
	}
	_, err := cfg.Spend(context.Background())
	if !errors.Is(err, llm.ErrSpendNotApplicable) {
		t.Errorf("err = %v, want ErrSpendNotApplicable", err)
	}
	if errors.Is(err, llm.ErrSpendUnavailable) {
		t.Error("a local backend must not report ErrSpendUnavailable — nothing failed")
	}
}

func TestConfigSpendDoesNotSendRetainedKeyToOllamaCloud(t *testing.T) {
	cfg := llm.Config{
		Provider:   "ollama",
		BaseURL:    "http://127.0.0.1:1/v1",
		APIKey:     "retained-openrouter-key",
		CostStatus: llm.CostStatusSubscriptionIncluded,
	}
	_, err := cfg.Spend(context.Background())
	if !errors.Is(err, llm.ErrSpendSubscriptionIncluded) {
		t.Fatalf("err = %v, want ErrSpendSubscriptionIncluded without a network call", err)
	}
}
