package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestStdioClientImplementsTransportAndPing(t *testing.T) {
	var _ Transport = (*Client)(nil)
	c, cleanup := newTestPair(t)
	defer cleanup()
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestHTTPClientImplementsTransport(t *testing.T) {
	var _ Transport = (*HTTPClient)(nil)
}

func TestHTTPAuthFromEnv(t *testing.T) {
	cases := []struct {
		name        string
		env         []string
		wantHeaders map[string]string
		wantBearer  string
	}{
		{"empty", nil, nil, ""},
		{"bearer-only", []string{"MCP_BEARER_TOKEN=secret-tok"}, nil, "secret-tok"},
		{
			"single-header",
			[]string{"MCP_HEADER_X_API_KEY=key-1"},
			map[string]string{"X-API-KEY": "key-1"},
			"",
		},
		{
			"header-and-bearer",
			[]string{"MCP_HEADER_X_API_KEY=key-1", "MCP_BEARER_TOKEN=tok"},
			map[string]string{"X-API-KEY": "key-1"},
			"tok",
		},
		{
			"multi-underscore-header",
			[]string{"MCP_HEADER_X_TENANT_ID=acme"},
			map[string]string{"X-TENANT-ID": "acme"},
			"",
		},
		{
			"value-with-equals-preserved",
			[]string{"MCP_BEARER_TOKEN=a=b=c"},
			nil,
			"a=b=c",
		},
		{
			"malformed-entry-skipped",
			[]string{"NOT_A_PAIR", "MCP_BEARER_TOKEN=ok"},
			nil,
			"ok",
		},
		{
			"unrelated-env-ignored",
			[]string{"PATH=/usr/bin", "HOME=/root"},
			nil,
			"",
		},
		{
			"empty-header-suffix-ignored",
			[]string{"MCP_HEADER_=oops"},
			nil,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers, bearer := httpAuthFromEnv(tc.env)
			if bearer != tc.wantBearer {
				t.Fatalf("bearer = %q, want %q", bearer, tc.wantBearer)
			}
			if !reflect.DeepEqual(headers, tc.wantHeaders) {
				t.Fatalf("headers = %#v, want %#v", headers, tc.wantHeaders)
			}
		})
	}
}

func TestOpenServerDispatchesToHTTP(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "sid-http", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.Header.Get("Authorization") != "Bearer env-tok" {
			t.Fatalf("Authorization = %q, want env-derived bearer", r.Header.Get("Authorization"))
		}
		writeHTTPRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{
			{"name": "remote_echo", "inputSchema": map[string]any{"type": "object"}},
		}})
	}))
	defer srv.Close()

	tr, err := OpenServer(context.Background(), "remote", ManagedServer{
		Type: ServerTypeStreamableHTTP,
		URL:  srv.URL,
		Env:  []string{"MCP_BEARER_TOKEN=env-tok"},
	})
	if err != nil {
		t.Fatalf("OpenServer (http): %v", err)
	}
	defer func() { _ = tr.Close() }()
	tools, err := tr.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "remote_echo" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestOpenServerDispatchesToStdio(t *testing.T) {
	tr, err := OpenServer(context.Background(), "local", ManagedServer{
		Command: helperServerConfig("").Command,
		Args:    helperServerConfig("").Args,
		Env:     helperServerConfig("").Env,
	})
	if err != nil {
		t.Fatalf("OpenServer (stdio): %v", err)
	}
	defer func() { _ = tr.Close() }()
	if _, ok := tr.(*Client); !ok {
		t.Fatalf("OpenServer returned %T, want *Client for stdio", tr)
	}
	if err := tr.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenServerStdioEmptyCommandErrors(t *testing.T) {
	_, err := OpenServer(context.Background(), "blank", ManagedServer{Type: ServerTypeStdio})
	if err == nil || !strings.Contains(err.Error(), "empty command") {
		t.Fatalf("want empty command error, got %v", err)
	}
}
