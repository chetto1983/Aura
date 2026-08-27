package arcadedb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// handleTransactionEndpoints answers ArcadeDB's explicit-transaction
// lifecycle (transaction.go: begin/commit/rollback) with a synthetic
// session before a mock server's own statement-recording/routing logic
// ever sees the request. These mocks exist to assert on the SQL statements
// attachFactSourceOnce sends, not to model ArcadeDB's transaction protocol
// itself (that is exercised for real only against the live server, e.g.
// TestZZProbeRawAppendConcurrency and the concurrent fan-out tests under
// the arcadedb_integration tag). It reports whether it handled the request
// so the caller can return early rather than recording/routing it.
func handleTransactionEndpoints(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case strings.Contains(r.URL.Path, "/api/v1/begin/"):
		w.Header().Set("arcadedb-session-id", "AS-mock-session")
		w.WriteHeader(http.StatusNoContent)
		return true
	case strings.Contains(r.URL.Path, "/api/v1/commit/"), strings.Contains(r.URL.Path, "/api/v1/rollback/"):
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

type recordedRequest struct {
	Method   string
	Path     string
	RawQuery string
	Auth     string
	Payload  map[string]any
}

type testResponse struct {
	Status int
	Body   string
}

func routedClient(
	t *testing.T,
	respond func(recordedRequest) testResponse,
) (*Client, *[]recordedRequest) {
	t.Helper()
	requests := []recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleTransactionEndpoints(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		request := recordedRequest{
			Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery,
			Auth: r.Header.Get("Authorization"), Payload: map[string]any{},
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &request.Payload)
		}
		requests = append(requests, request)
		response := respond(request)
		if response.Status == 0 {
			response.Status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.Status)
		_, _ = io.WriteString(w, response.Body)
	}))
	t.Cleanup(srv.Close)
	return mustClient(t, srv.URL), &requests
}
