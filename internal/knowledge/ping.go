// Health probes for the graph substrate (Pattern 5). Ping runs two checks: the
// MCP/Neo4j liveness call (dbms.components, asserts a 5.26.x kernel) and the
// embed-sidecar dim self-test. The dim self-test is the ONLY place the
// AURA_EMBED_DIMENSIONS=768 contract becomes operational (Amendment #18 /
// Pitfall #5): a sidecar returning the wrong dimension would silently corrupt
// the HNSW index, so we refuse to start on mismatch with a load-bearing error.
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PingResult carries what the CLI prints on a healthy ping.
type PingResult struct {
	ServerVersion string
	EmbedDim      int
}

// Ping verifies the MCP/Neo4j channel then the embed sidecar dimension.
func Ping(ctx context.Context, mcp *Client, cfg *Config) (*PingResult, error) {
	version, err := pingMCP(ctx, mcp)
	if err != nil {
		return nil, err
	}
	if err := pingEmbed(ctx, cfg.EmbedURL, cfg.EmbedDimensions); err != nil {
		return nil, err
	}
	return &PingResult{ServerVersion: version, EmbedDim: cfg.EmbedDimensions}, nil
}

// pingMCP calls dbms.components through the MCP read tool and returns the Neo4j
// kernel version, asserting the 5.26.x line (Amendment #2 LTS pin).
func pingMCP(ctx context.Context, mcp *Client) (string, error) {
	raw, err := mcp.Cypher(ctx, "CALL dbms.components() YIELD name, versions, edition", nil, false)
	if err != nil {
		return "", err
	}
	rows, err := decodeRows(raw)
	if err != nil {
		return "", err
	}
	version := kernelVersion(rows)
	if version == "" {
		return "", fmt.Errorf("dbms.components returned no Neo4j Kernel version row")
	}
	if !strings.HasPrefix(version, "5.26") {
		return "", fmt.Errorf("unexpected Neo4j version %q (want 5.26.x — Amendment #2 LTS pin)", version)
	}
	return version, nil
}

// kernelVersion extracts the first version string from the "Neo4j Kernel" row
// (versions is a JSON array). Tolerant of row ordering and missing fields.
func kernelVersion(rows []map[string]any) string {
	for _, r := range rows {
		if name, _ := r["name"].(string); name != "Neo4j Kernel" {
			continue
		}
		versions, ok := r["versions"].([]any)
		if !ok || len(versions) == 0 {
			continue
		}
		if v, ok := versions[0].(string); ok {
			return v
		}
	}
	return ""
}

// pingEmbed POSTs a dummy input to the sidecar's /v1/embeddings and asserts the
// returned vector length equals expectedDim. Mismatch is a refuse-to-start error
// with the load-bearing literal (Pattern 5 / T-1.07-05). Unit-tested against a
// mocked sidecar by TestPingEmbed_DimMismatch.
func pingEmbed(ctx context.Context, baseURL string, expectedDim int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/embeddings",
		bytes.NewReader([]byte(`{"input":"ping","model":"embedding"}`)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	// Drop the pooled keep-alive connection on return; otherwise the default
	// transport's persistConn read/write goroutines linger until IdleConnTimeout
	// and make goleak.VerifyTestMain order-dependent (passes in the full suite,
	// trips on short subsets).
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("embed sidecar unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("embed sidecar returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("embed sidecar decode: %w", err)
	}
	actual := 0
	if len(body.Data) > 0 {
		actual = len(body.Data[0].Embedding)
	}
	if actual != expectedDim {
		return fmt.Errorf(
			"embedding sidecar returned dim=%d but AURA_EMBED_DIMENSIONS=%d — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook",
			actual, expectedDim)
	}
	return nil
}
