package embeddings

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
)

const (
	fakeBOS = 2
	fakeEOS = 1
)

// fakeSidecar holds the llama.cpp embedding server to the contract measured on b10951:
// the catalogue publishes meta.n_ctx, /tokenize wraps the text in BOS and EOS, token-ID
// input is embedded as sent, and an input above n_ctx tokens is refused with HTTP 500
// together with the batch around it. Its tokenizer spends one token per rune, so a token
// never costs less than a byte, as measured on EmbeddingGemma.
type fakeSidecar struct {
	t    testing.TB
	nCtx int

	mu        sync.Mutex
	requests  [][]any
	tokenized int
	listed    int
	catalogue int // HTTP status for the catalogue; 0 means 200
}

func newFakeSidecar(t testing.TB, nCtx int) (*fakeSidecar, *httptest.Server) {
	t.Helper()
	fake := &fakeSidecar{t: t, nCtx: nCtx}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

// snapshot reads under the handler's lock: a request's return over loopback TCP is not a
// happens-before edge the race detector can see.
func (f *fakeSidecar) snapshot() (requests [][]any, tokenized, listed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]any(nil), f.requests...), f.tokenized, f.listed
}

func (f *fakeSidecar) setCatalogueStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogue = status
}

func fakeTokens(text string) []int {
	ids := []int{fakeBOS}
	for _, r := range text {
		ids = append(ids, 1000+int(r))
	}
	return append(ids, fakeEOS)
}

func (f *fakeSidecar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/models":
		f.listed++
		if f.catalogue != 0 {
			w.WriteHeader(f.catalogue)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"/models/embedding.gguf","meta":{"n_ctx":%d}}]}`, f.nCtx)
	case "/tokenize":
		f.tokenized++
		var body struct {
			Content    string `json:"content"`
			AddSpecial bool   `json:"add_special"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.AddSpecial {
			f.t.Errorf("tokenize body = %+v, %v; want add_special", body, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": fakeTokens(body.Content)})
	case "/v1/embeddings":
		var body struct {
			Input []any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("decode embeddings: %v", err)
		}
		f.requests = append(f.requests, body.Input)
		data := make([]map[string]any, len(body.Input))
		for index, input := range body.Input {
			count := inputTokens(input)
			if count > f.nCtx {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, `{"error":{"message":"input (%d tokens) is too large to process"}}`, count)
				return
			}
			data[index] = map[string]any{"index": index, "embedding": []float64{float64(count), 1}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	default:
		f.t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func inputTokens(input any) int {
	switch value := input.(type) {
	case string:
		return len(fakeTokens(value))
	case []any:
		return len(value)
	}
	return 0
}

func localClient(server *httptest.Server) *Client {
	return &Client{BaseURL: server.URL, Client: server.Client(), Dimensions: 2}
}

func TestClientSendsInputsWithinTheLimitUnchanged(t *testing.T) {
	fake, server := newFakeSidecar(t, 16)
	// "short" fits by bytes alone; "ééééééééé" is 18 bytes, so only the tokenizer can
	// tell that its 11 tokens still fit.
	vectors, err := localClient(server).Embed(t.Context(), []string{"short", "ééééééééé"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, tokenized, _ := fake.snapshot()
	if len(requests) != 1 || requests[0][0] != "short" || requests[0][1] != "ééééééééé" {
		t.Fatalf("requests = %v, want both texts sent unchanged", requests)
	}
	if tokenized != 1 {
		t.Fatalf("tokenize calls = %d, want 1 (only the input bytes cannot clear)", tokenized)
	}
	if vectors[0][0] != 7 || vectors[1][0] != 11 {
		t.Fatalf("vectors = %v", vectors)
	}
}

func TestClientSendsLeadingTokensOfAnInputOverTheLimit(t *testing.T) {
	fake, server := newFakeSidecar(t, 16)
	text := strings.Repeat("abcdefghij", 4)
	if _, err := localClient(server).Embed(t.Context(), []string{text}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, _, _ := fake.snapshot()
	sent, ok := requests[0][0].([]any)
	if !ok || len(sent) != 16 {
		t.Fatalf("sent = %#v, want 16 token IDs", requests[0][0])
	}
	want := append(fakeTokens(text)[:15], fakeEOS)
	for index, id := range sent {
		if int(id.(float64)) != want[index] {
			t.Fatalf("token %d = %v, want %d (BOS, leading content, EOS)", index, id, want[index])
		}
	}
}

// An oversized input used to take its whole batch down with HTTP 500, and the fact backfill
// selects the same rows on every sweep, so one such fact stalled the rest forever.
func TestClientOversizedInputNoLongerFailsItsBatch(t *testing.T) {
	_, server := newFakeSidecar(t, 64)
	vectors, err := localClient(server).Embed(t.Context(),
		[]string{"before", strings.Repeat("x", 3000), "after"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 3 || vectors[0][0] != 8 || vectors[1][0] != 64 || vectors[2][0] != 7 {
		t.Fatalf("vectors = %v", vectors)
	}
}

func TestClientPacksRequestsByTokenBudget(t *testing.T) {
	fake, server := newFakeSidecar(t, 2048)
	texts := make([]string, 5)
	for index := range texts {
		texts[index] = strings.Repeat(string(rune('a'+index)), 1500)
	}
	vectors, err := localClient(server).Embed(t.Context(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	requests, _, _ := fake.snapshot()
	sizes := make([]int, len(requests))
	for index, request := range requests {
		sizes[index] = len(request)
	}
	if fmt.Sprint(sizes) != "[2 2 1]" {
		t.Fatalf("request sizes = %v, want [2 2 1] under a %d-token budget", sizes, requestTokenBudget)
	}
	if requests[2][0] != texts[4] || len(vectors) != 5 {
		t.Fatalf("order lost: last request %v, %d vectors", requests[2], len(vectors))
	}
}

func TestClientReadsTheLimitOnceAndRetriesAFailedLookup(t *testing.T) {
	fake, server := newFakeSidecar(t, 64)
	fake.setCatalogueStatus(http.StatusServiceUnavailable)
	client := localClient(server)
	_, err := client.Embed(t.Context(), []string{"private note"})
	if err == nil || !strings.Contains(err.Error(), "input limit") || strings.Contains(err.Error(), "private note") {
		t.Fatalf("lookup error = %v, want a named input-limit failure without the text", err)
	}
	if requests, _, _ := fake.snapshot(); len(requests) != 0 {
		t.Fatalf("embedded %v without knowing the limit", requests)
	}
	fake.setCatalogueStatus(0)
	for range 2 {
		if _, err := client.Embed(t.Context(), []string{"private note"}); err != nil {
			t.Fatalf("Embed after recovery: %v", err)
		}
	}
	if _, _, listed := fake.snapshot(); listed != 2 {
		t.Fatalf("catalogue reads = %d, want 2 (the failure, then one cached success)", listed)
	}
}

// hostedServer answers as OpenRouter does: the embedding models live under
// /v1/embeddings/models, and /tokenize does not exist. The returned func reads every
// input embedded so far.
func hostedServer(t *testing.T, catalogue string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("%s without the bearer key", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/v1/embeddings/models":
			_, _ = io.WriteString(w, catalogue)
		case "/api/v1/embeddings":
			var body struct {
				Input []string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			sent = append(sent, body.Input...)
			mu.Unlock()
			data := make([]map[string]any, len(body.Input))
			for index := range body.Input {
				data[index] = map[string]any{"index": index, "embedding": []float64{1, 0}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
}

func TestClientHostedRouteCutsOnAUTF8BoundaryUnderTheProviderLimit(t *testing.T) {
	server, sent := hostedServer(t, `{"data":[
		{"id":"vendor/other","context_length":8},
		{"id":"vendor/embed","context_length":32,"top_provider":{"context_length":24}}]}`)
	client := &Client{BaseURL: server.URL + "/api", Model: "vendor/embed:nitro", APIKey: "key",
		Client: server.Client(), Dimensions: 2}
	// 21 ASCII bytes then "é": byte 22, the cut point for a 24-token limit, is mid-rune.
	text := strings.Repeat("a", 21) + "é" + strings.Repeat("b", 10)
	if _, err := client.Embed(t.Context(), []string{"short", text}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got := sent(); len(got) != 2 || got[0] != "short" || got[1] != strings.Repeat("a", 21) {
		t.Fatalf("sent = %q, want the long text cut back to the rune boundary before byte 22", got)
	}
}

func TestClientRefusesACatalogueThatDoesNotServeTheModel(t *testing.T) {
	server, sent := hostedServer(t, `{"data":[{"id":"a","context_length":512},{"id":"b","context_length":512}]}`)
	client := &Client{BaseURL: server.URL + "/api", Model: "vendor/embed", APIKey: "key",
		Client: server.Client(), Dimensions: 2}
	_, err := client.Embed(t.Context(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "vendor/embed") || len(sent()) != 0 {
		t.Fatalf("err = %v, sent = %v; want a refusal naming the model before any embedding", err, sent())
	}
}

func TestPropertyNoInputReachesTheModelAboveItsLimit(t *testing.T) {
	alphabet := rapid.SampledFrom([]rune{'a', ' ', '\n', 'é', '€', '😀', '中'})
	rapid.Check(t, func(rt *rapid.T) {
		nCtx := rapid.IntRange(4, 48).Draw(rt, "nCtx")
		texts := rapid.SliceOfN(
			rapid.StringOfN(alphabet, 1, 3*nCtx, -1), 1, 40).Draw(rt, "texts")
		fake := &fakeSidecar{t: t, nCtx: nCtx}
		server := httptest.NewServer(fake)
		defer server.Close()
		client := localClient(server)
		client.BatchSize = rapid.IntRange(1, 8).Draw(rt, "batch")

		vectors, err := client.Embed(t.Context(), texts)
		if err != nil {
			rt.Fatalf("Embed: %v", err)
		}
		if len(vectors) != len(texts) {
			rt.Fatalf("%d vectors for %d texts", len(vectors), len(texts))
		}
		for index, text := range texts {
			full := len(fakeTokens(text))
			if want := min(full, nCtx); int(vectors[index][0]) != want {
				rt.Fatalf("text %d embedded %v tokens, want %d", index, vectors[index][0], want)
			}
		}
		requests, _, _ := fake.snapshot()
		for _, request := range requests {
			if len(request) > client.BatchSize {
				rt.Fatalf("request of %d inputs exceeds batch size %d", len(request), client.BatchSize)
			}
		}
	})
}

func TestPropertyByteCutIsAValidUTF8PrefixWithinTheBudget(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		text := rapid.String().Draw(rt, "text")
		budget := rapid.IntRange(0, len(text)+4).Draw(rt, "budget")
		cut := cutUTF8(text, budget)
		if len(cut) > budget || !strings.HasPrefix(text, cut) {
			rt.Fatalf("cut %q of %q exceeds %d bytes or is not a prefix", cut, text, budget)
		}
		if utf8.ValidString(text) && !utf8.ValidString(cut) {
			rt.Fatalf("cut %q of valid %q split a rune", cut, text)
		}
		if len(text) <= budget && cut != text {
			rt.Fatalf("text within budget was cut: %q -> %q", text, cut)
		}
	})
}
