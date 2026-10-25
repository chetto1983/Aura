package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
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
	for i := range 21 {
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
	// The last switch installed the second route; the reader must have followed it there.
	if _, model, err := describeOnce(t, vision); err != nil || model != "qwen3-vl" {
		t.Fatalf("after the switches the model is %q (err %v), want qwen3-vl", model, err)
	}
}

func TestLiveVisionReadsWithOpenRouterAfterASwitch(t *testing.T) {
	var mu sync.Mutex
	var bearer, sentModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			_, _ = io.WriteString(w, `{"data":[`+
				`{"id":"vendor/sees","architecture":{"input_modalities":["text","image"]}},`+
				`{"id":"vendor/blind","architecture":{"input_modalities":["text"]}}]}`)
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			bearer, sentModel = r.Header.Get("Authorization"), body.Model
			mu.Unlock()
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"read on openrouter"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	// ReasoningTarget reads any base URL that names openrouter.ai as OpenRouter.
	openRouter := llm.Config{Provider: "openrouter", BaseURL: srv.URL + "/openrouter.ai/api/v1"}
	route, _ := ollamaVision(t, "gemma4", "read by gemma")
	sidecar, _ := visionSidecar(t)
	creds, _ := storedCredentials(map[string]string{settings.ServicesKeySetting: "services-key"})
	runtime := llm.NewRuntime(nil, route)
	vision := liveImageReader(&chatEnv{
		cfg:         &config.Config{MultimodalTimeoutSec: 5, MultimodalBaseURL: sidecar, MultimodalModel: "glm-ocr"},
		credentials: creds, llmRuntime: runtime,
	})
	if _, model, err := describeOnce(t, vision); err != nil || model != "gemma4" {
		t.Fatalf("boot route read model %q, err %v; want gemma4", model, err)
	}

	openRouter.Model = "vendor/sees"
	runtime.Replace(nil, openRouter)
	text, model, err := describeOnce(t, vision)
	if err != nil || text != "read on openrouter" || model != "vendor/sees" {
		t.Fatalf("OpenRouter read = %q, %q, %v; want the new model's text", text, model, err)
	}
	mu.Lock()
	gotBearer, gotModel := bearer, sentModel
	mu.Unlock()
	if gotBearer != "Bearer services-key" || gotModel != "vendor/sees" {
		t.Fatalf("OpenRouter was sent %q for model %q, want the services key for vendor/sees", gotBearer, gotModel)
	}

	openRouter.Model = "vendor/blind"
	runtime.Replace(nil, openRouter)
	text, model, err = describeOnce(t, vision)
	if err != nil || text != "read by the sidecar" || model != "glm-ocr" {
		t.Fatalf("a text-only OpenRouter model = %q, %q, %v; want the sidecar", text, model, err)
	}
}
