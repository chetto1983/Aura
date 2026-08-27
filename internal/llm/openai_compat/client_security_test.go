package openai_compat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestSecretRedaction (D-28, T-03-01, release-blocking): the sentinel API key
// must appear in NO Chunk, NO HTTPError, NO error string. The Authorization
// header is the ONLY place the key is written (TestRequestBody covers that it is
// the request, never a logged/returned struct).
func TestSecretRedaction(t *testing.T) {
	// Success path: assert no chunk carries the key.
	okSrv := httptest.NewServer(fixtureHandler(t, "toolcall_multichunk.sse"))
	defer okSrv.Close()
	c := New(testConfig(okSrv.URL))
	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for _, ck := range drain(ch) {
		blob, _ := json.Marshal(ck)
		if strings.Contains(string(blob), sentinelKey) {
			t.Fatalf("API key leaked into a Chunk: %s", blob)
		}
	}

	// Error path: assert the key is absent from HTTPError + its Error() string.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer errSrv.Close()
	_, herr := New(testConfig(errSrv.URL)).Stream(context.Background(), llm.Request{Model: "m"})
	if herr == nil {
		t.Fatal("want error on 401")
	}
	if strings.Contains(herr.Error(), sentinelKey) {
		t.Fatalf("API key leaked into HTTPError.Error(): %s", herr.Error())
	}
	blob, _ := json.Marshal(herr)
	if strings.Contains(string(blob), sentinelKey) {
		t.Fatalf("API key leaked into serialized HTTPError: %s", blob)
	}
}
