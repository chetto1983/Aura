// Unit-safe tests (B1 split, Nyquist <30s feedback): NO build tag, so these run
// in every `go test ./internal/knowledge/...` invocation without docker, a live
// Neo4j, or the MCP subprocess. They cover the security-relevant client surface
// (dim self-test, stderr redaction, no-concat query API) plus the compose
// loopback gate. This file owns the package's sole TestMain (goleak) — it is
// compiled in BOTH the untagged and the neo4j_integration build, so the
// integration tests in client_test.go inherit goleak leak-detection from here
// (a second TestMain there would be a duplicate symbol).
package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TestPingEmbed_DimMismatch asserts the Pattern 5 self-test refuses a wrong-dim
// sidecar with the load-bearing literal error (T-1.07-05). The sidecar is mocked
// to return a 768-element vector against the default 384.
func TestPingEmbed_DimMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var sb strings.Builder
		sb.WriteString(`{"data":[{"embedding":[`)
		for i := range 768 {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("0.1")
		}
		sb.WriteString(`]}]}`)
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer srv.Close()

	err := pingEmbed(context.Background(), srv.URL, DefaultEmbedDimensions)
	if err == nil {
		t.Fatal("pingEmbed: want error on dim mismatch, got nil")
	}
	for _, want := range []string{
		"embedding sidecar returned dim=768",
		"AURA_EMBED_DIMENSIONS=384",
		"refuse to start (Pitfall #7 silent corruption)",
		"amendment #18 swap runbook",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\n  got: %v", want, err)
		}
	}
}

// TestPingEmbed_MatchOK asserts the happy path passes when dim matches.
func TestPingEmbed_MatchOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var sb strings.Builder
		sb.WriteString(`{"data":[{"embedding":[`)
		for i := range 8 {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("0.1")
		}
		sb.WriteString(`]}]}`)
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer srv.Close()

	if err := pingEmbed(context.Background(), srv.URL, 8); err != nil {
		t.Fatalf("pingEmbed: want nil on dim match, got %v", err)
	}
}

// TestClient_StderrRedaction asserts captured stderr never leaks the literal
// password or password=/pass: assignments (T-1.07-04).
func TestClient_StderrRedaction(t *testing.T) {
	c := &Client{password: "sup3r-s3cret"}
	got := c.redactSecrets("auth failed password=sup3r-s3cret for user neo4j; pass: sup3r-s3cret")
	if strings.Contains(got, "sup3r-s3cret") {
		t.Errorf("redactSecrets leaked the password: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("redactSecrets did not insert a mask marker: %q", got)
	}

	// Even without the literal known, password=VALUE assignments must be masked.
	c2 := &Client{}
	got2 := c2.redactSecrets("dsn error password=hunter2&db=neo4j")
	if strings.Contains(got2, "hunter2") {
		t.Errorf("redactSecrets leaked an assignment value: %q", got2)
	}
}

// TestCypherClient_RejectsConcatenatedInjection asserts the public client API
// binds params structurally and never concatenates them into the query body
// (T-1.07-01): the query is passed verbatim, the param rides arguments.params.
func TestCypherClient_RejectsConcatenatedInjection(t *testing.T) {
	c := &Client{}
	const query = "MERGE (c:Chunk {id: $id}) RETURN c"
	injection := "x'); DROP DATABASE neo4j; //"
	req := c.buildRequest(query, map[string]any{"id": injection}, true)

	params, ok := req.Params.(map[string]any)
	if !ok {
		t.Fatalf("req.Params is not a map: %T", req.Params)
	}
	args, ok := params["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("arguments is not a map: %T", params["arguments"])
	}
	if args["query"] != query {
		t.Errorf("query was mutated: want %q, got %q", query, args["query"])
	}
	bound, ok := args["params"].(map[string]any)
	if !ok {
		t.Fatalf("arguments.params is not a map: %T", args["params"])
	}
	if bound["id"] != injection {
		t.Errorf("injection not bound as a param: got %v", bound["id"])
	}
	if params["name"] != toolWrite {
		t.Errorf("write call should use %q, got %v", toolWrite, params["name"])
	}
}

// caddyPublicIngress is the ONE sanctioned non-loopback published port: the caddy
// reverse-proxy HTTPS front door of the Phase-17 appliance topology, gated by
// AURA_ACCESS_TOKEN. Every other published port must stay loopback-only.
const caddyPublicIngress = `0.0.0.0:${AURA_HTTPS_PORT:-443}:443`

// TestComposeYAML_LoopbackOnly reads the repo compose.yaml and asserts every published
// host port binds to 127.0.0.1 — except the single sanctioned caddy public ingress
// (T-1.07-02 / T-1.07-03). Pure file I/O.
func TestComposeYAML_LoopbackOnly(t *testing.T) {
	data, err := os.ReadFile("../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		// Port mappings look like:  - "127.0.0.1:5432:5432" or with ${VAR:-127.0.0.1}.
		if !strings.HasPrefix(trimmed, "- \"") {
			continue
		}
		mapping := strings.Trim(strings.TrimPrefix(trimmed, "- "), "\"")
		// Skip non-port-map quoted scalars (no two colons separating host:port:port).
		if strings.Count(mapping, ":") < 2 {
			continue
		}
		// The caddy HTTPS ingress is the one sanctioned public port (appliance front
		// door, token-gated); everything else must default to loopback.
		if mapping == caddyPublicIngress {
			continue
		}
		if !strings.Contains(mapping, "127.0.0.1") {
			t.Errorf("non-loopback port mapping found: %q", mapping)
		}
	}
}
