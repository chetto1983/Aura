package arcadedb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
