package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// ollamaVision serves an Ollama that reads images: /api/show names the vision capability and
// every read answers with answer. shows counts the capability probes.
func ollamaVision(t *testing.T, model, answer string) (cfg llm.Config, shows *atomic.Int32) {
	t.Helper()
	shows = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			shows.Add(1)
			_, _ = io.WriteString(w, `{"capabilities":["completion","vision"]}`)
		case "/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+answer+`"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return llm.Config{Provider: "ollama", BaseURL: srv.URL + "/v1", Model: model}, shows
}

// visionSidecar serves the local aura-ocr-vl arm; calls counts its reads.
func visionSidecar(t *testing.T) (baseURL string, calls *atomic.Int32) {
	t.Helper()
	calls = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"read by the sidecar"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1", calls
}

func describeOnce(t *testing.T, vision *liveVision) (string, string, error) {
	t.Helper()
	return vision.Describe(t.Context(), []byte("image"), "image/png", "describe")
}

func TestLiveVisionReadsWithTheRouteInForce(t *testing.T) {
	first, _ := ollamaVision(t, "gemma4", "read by gemma")
	second, _ := ollamaVision(t, "qwen3-vl", "read by qwen")
	runtime := llm.NewRuntime(nil, first)
	vision := liveImageReader(&chatEnv{cfg: &config.Config{MultimodalTimeoutSec: 5}, llmRuntime: runtime})

	text, model, err := describeOnce(t, vision)
	if err != nil || text != "read by gemma" || model != "gemma4" {
		t.Fatalf("boot route read = %q, %q, %v; want gemma's text and model", text, model, err)
	}
	runtime.Replace(nil, second)
	text, model, err = describeOnce(t, vision)
	if err != nil || text != "read by qwen" || model != "qwen3-vl" {
		t.Fatalf("after the switch = %q, %q, %v; want qwen's text and model", text, model, err)
	}
}

func TestLiveVisionAsksTheCapabilityOncePerRoute(t *testing.T) {
	route, shows := ollamaVision(t, "gemma4", "ok")
	runtime := llm.NewRuntime(nil, route)
	vision := liveImageReader(&chatEnv{cfg: &config.Config{MultimodalTimeoutSec: 5}, llmRuntime: runtime})

	for range 2 {
		if _, _, err := describeOnce(t, vision); err != nil {
			t.Fatal(err)
		}
	}
	if got := shows.Load(); got != 1 {
		t.Fatalf("two reads on one route asked the capability %d times, want 1", got)
	}
	runtime.Replace(nil, route)
	if _, _, err := describeOnce(t, vision); err != nil {
		t.Fatal(err)
	}
	if got := shows.Load(); got != 2 {
		t.Fatalf("a new route was asked %d times in all, want 2", got)
	}
}

func TestLiveVisionReadsWithChatGPTAfterASwitch(t *testing.T) {
	route, _ := ollamaVision(t, "gemma4", "read by gemma")
	sidecar, calls := visionSidecar(t)
	cfg := &config.Config{
		MultimodalTimeoutSec: 5, MultimodalBaseURL: sidecar, MultimodalModel: "glm-ocr",
		SkillsDir:     filepath.Join(t.TempDir(), "skills"),
		AuthulaSecret: "00000000000000000000000000000000000000000000000000000000000000a1",
	}
	plan := newChatGPTPlan(cfg)
	if plan == nil {
		t.Fatal("connection service not created")
	}
	chatGPT := llm.Config{Provider: llm.ChatGPTProvider, BaseURL: llm.ChatGPTBaseURL, Model: "gpt-6.1-sol"}

	runtime := llm.NewRuntime(nil, route)
	vision := liveImageReader(&chatEnv{cfg: cfg, llmRuntime: runtime, chatGPTPlan: plan})
	if _, model, err := describeOnce(t, vision); err != nil || model != "gemma4" {
		t.Fatalf("boot route read model %q, err %v; want gemma4", model, err)
	}
	runtime.Replace(nil, chatGPT)
	// No identity on the context is a failure of the ChatGPT reader, which never hands the image
	// to the sidecar: the reader, not the HTTP arms, answered.
	if _, _, err := describeOnce(t, vision); err == nil {
		t.Fatal("the ChatGPT route read an image without an owner")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("the sidecar was called %d times on the ChatGPT route", got)
	}

	withoutPlan := liveImageReader(&chatEnv{cfg: cfg, llmRuntime: llm.NewRuntime(nil, chatGPT)})
	text, model, err := describeOnce(t, withoutPlan)
	if err != nil || text != "read by the sidecar" || model != "glm-ocr" {
		t.Fatalf("ChatGPT without a connection = %q, %q, %v; want the sidecar", text, model, err)
	}
}

func TestLiveVisionSurvivesConcurrentSwitches(t *testing.T) {
	first, _ := ollamaVision(t, "gemma4", "read by gemma")
	second, _ := ollamaVision(t, "qwen3-vl", "read by qwen")
	runtime := llm.NewRuntime(nil, first)
	vision := liveImageReader(&chatEnv{cfg: &config.Config{MultimodalTimeoutSec: 5}, llmRuntime: runtime})

	var wg sync.WaitGroup
	errs := make(chan error, 8*5)
	for range 8 {
		wg.Go(func() {
			for range 5 {
				if _, _, err := describeOnce(t, vision); err != nil {
					errs <- err
				}
			}
		})
	}
	for i := range 20 {
		if i%2 == 0 {
			runtime.Replace(nil, second)
		} else {
			runtime.Replace(nil, first)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("read during a switch: %v", err)
	}
}
