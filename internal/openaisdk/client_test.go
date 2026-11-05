package openaisdk

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientCarriesNoAmbientCredentialAndNeverRetries(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "ambient-services-key")
	t.Setenv("OPENAI_ADMIN_KEY", "ambient-admin-key")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "X-Ambient: leaked")
	var hits atomic.Int32
	var paths []string
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		paths, header = append(paths, r.URL.Path), r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	t.Cleanup(srv.Close)

	for _, base := range []string{srv.URL + "/api/v1", srv.URL + "/api/v1/"} {
		err := Client(srv.Client(), base).Post(t.Context(), "things", map[string]string{}, nil)
		require.Error(t, err)
	}

	assert.EqualValues(t, 2, hits.Load(), "one request per call, no retry of the 500")
	assert.Equal(t, []string{"/api/v1/things", "/api/v1/things"}, paths)
	assert.Empty(t, header.Get("Authorization"))
	assert.Empty(t, header.Get("X-Ambient"))
}

func TestClientSendsTheCallersKey(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	err := Client(srv.Client(), srv.URL, option.WithAPIKey("k")).Post(t.Context(), "things", map[string]string{}, nil)

	require.NoError(t, err)
	assert.Equal(t, "Bearer k", auth)
}
