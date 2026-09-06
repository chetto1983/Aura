package agui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// capsServer builds a live server exposing GET /api/composer/reasoning-capabilities. A nil src
// leaves the source unwired (the safe-fallback path); a non-nil src is injected with its backend
// label exactly as the composition root does.
func capsServer(t *testing.T, src llm.ReasoningCapabilitySource, backend string) *httptest.Server {
	t.Helper()
	s := NewServer(nil, nil, ServerConfig{})
	if src != nil {
		s.SetReasoningCapabilitySource(src, backend)
	}
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

func fetchCaps(t *testing.T, srv *httptest.Server) (reasoningCapabilitiesDTO, string, int) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/composer/reasoning-capabilities")
	if err != nil {
		t.Fatalf("GET reasoning-capabilities: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var dto reasoningCapabilitiesDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return dto, string(raw), resp.StatusCode
}

// TestReasoningCapabilitiesEndpoint proves the handler maps advertised efforts → UI symbols
// (auto prepended, none→off … max→max), omits off for a mandatory model (none absent upstream),
// degrades to the safe floor {auto,off} at HTTP 200 on a nil source, and never leaks the model
// id / base URL / key.
func TestReasoningCapabilitiesEndpoint(t *testing.T) {
	t.Run("advertised {none,low,high} → auto-first UI symbols", func(t *testing.T) {
		src := fakeCapSource{
			efforts:  []llm.ReasoningEffort{llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortHigh},
			detected: true,
		}
		dto, _, status := fetchCaps(t, capsServer(t, src, "openrouter"))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if got := strings.Join(dto.Levels, ","); got != "auto,off,low,high" {
			t.Errorf("levels = %q, want auto,off,low,high (auto prepended, mapped in order)", got)
		}
		if dto.Default != "auto" || dto.Backend != "openrouter" || !dto.Detected {
			t.Errorf("dto = %+v, want default=auto backend=openrouter detected=true", dto)
		}
	})

	t.Run("full graduated set maps extra/max", func(t *testing.T) {
		src := fakeCapSource{efforts: []llm.ReasoningEffort{
			llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
			llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax,
		}, detected: true}
		dto, _, _ := fetchCaps(t, capsServer(t, src, "llamacpp"))
		if got := strings.Join(dto.Levels, ","); got != "auto,off,low,mid,high,extra,max" {
			t.Errorf("levels = %q, want the full 7-symbol set", got)
		}
		if dto.Backend != "llamacpp" {
			t.Errorf("backend = %q, want llamacpp", dto.Backend)
		}
	})

	t.Run("mandatory model omits off", func(t *testing.T) {
		// A mandatory model's source has already stripped none (excludeNone), so off is absent.
		src := fakeCapSource{efforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}, detected: true}
		dto, _, _ := fetchCaps(t, capsServer(t, src, "openrouter"))
		if got := strings.Join(dto.Levels, ","); got != "auto,low,high" {
			t.Errorf("levels = %q, want auto,low,high (off omitted for a mandatory model)", got)
		}
		for _, l := range dto.Levels {
			if l == "off" {
				t.Error("off must not appear for a mandatory model")
			}
		}
	})

	t.Run("nil source → safe floor {auto,off} at 200, not 503", func(t *testing.T) {
		dto, _, status := fetchCaps(t, capsServer(t, nil, ""))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (degrade, never 503)", status)
		}
		if got := strings.Join(dto.Levels, ","); got != "auto,off" {
			t.Errorf("levels = %q, want the safe floor auto,off", got)
		}
		if dto.Detected {
			t.Error("detected = true on a nil source; want false")
		}
	})

	t.Run("detection failed → safe floor {auto,off}", func(t *testing.T) {
		dto, _, status := fetchCaps(t, capsServer(t, fakeCapSource{detected: false}, "openrouter"))
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		if got := strings.Join(dto.Levels, ","); got != "auto,off" || dto.Detected {
			t.Errorf("dto = %+v, want floor auto,off detected=false", dto)
		}
	})

	t.Run("body never leaks the model id / base URL / key", func(t *testing.T) {
		src := fakeCapSource{efforts: []llm.ReasoningEffort{llm.ReasoningEffortLow}, detected: true}
		_, raw, _ := fetchCaps(t, capsServer(t, src, "openrouter"))
		for _, banned := range []string{"http://", "https://", "base_url", "api_key", "apiKey", "sk-", "model_id", "deepseek"} {
			if strings.Contains(strings.ToLower(raw), strings.ToLower(banned)) {
				t.Errorf("response body leaked %q: %s", banned, raw)
			}
		}
	})
}

// TestEffortToUISymbolCoversEveryModelledLevel pins the projection table itself rather than
// one route's rendering of it. The gap it closes is specific: "max" had no case exercised
// anywhere, so a level silently dropped to "" would have shown up as a selector missing an
// option and no test failing. The unmodelled default is asserted for the same reason — the
// caller relies on "" meaning DROP, and a symbol invented here reaches the selector as an
// option it cannot render.
func TestEffortToUISymbolCoversEveryModelledLevel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		effort llm.ReasoningEffort
		want   string
	}{
		{llm.ReasoningEffortNone, "off"},
		{llm.ReasoningEffortLow, "low"},
		{llm.ReasoningEffortMedium, "mid"},
		{llm.ReasoningEffortHigh, "high"},
		{llm.ReasoningEffortXHigh, "extra"},
		{llm.ReasoningEffortMax, "max"},
		{llm.ReasoningEffort("minimal"), ""},
	} {
		if got := effortToUISymbol(tc.effort); got != tc.want {
			t.Errorf("effortToUISymbol(%q) = %q, want %q", tc.effort, got, tc.want)
		}
	}
}
