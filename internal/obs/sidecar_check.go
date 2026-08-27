package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const sidecarResponseLimit = 64 << 10

// SidecarChecker verifies the part ordinary container healthchecks cannot prove:
// Prometheus is not merely ready, its latest Aura scrape succeeded, and Grafana's
// provisioned Prometheus/Tempo datasources reach those services. The checker uses
// only internal HTTP endpoints; the scheduler never needs the Docker socket.
type SidecarChecker struct {
	client        *http.Client
	tempoURL      string
	prometheusURL string
	grafanaURL    string
}

// NewSidecarChecker builds the live observability probe. The supplied base URLs are
// owned by the deployment composition root and are deliberately not model-facing.
func NewSidecarChecker(client *http.Client, tempoURL, prometheusURL, grafanaURL string) *SidecarChecker {
	if client == nil {
		client = http.DefaultClient
	}
	return &SidecarChecker{
		client:        client,
		tempoURL:      strings.TrimRight(tempoURL, "/"),
		prometheusURL: strings.TrimRight(prometheusURL, "/"),
		grafanaURL:    strings.TrimRight(grafanaURL, "/"),
	}
}

// Check requires Tempo and Prometheus readiness plus one successful Aura target.
func (c *SidecarChecker) Check(ctx context.Context) error {
	if err := c.probe(ctx, c.tempoURL+"/ready", "tempo"); err != nil {
		return err
	}
	if err := c.probe(ctx, c.prometheusURL+"/-/ready", "prometheus"); err != nil {
		return err
	}
	queryURL := c.prometheusURL + "/api/v1/query?query=" + url.QueryEscape(`up{job="aura"}`)
	if err := c.requireAuraUp(ctx, queryURL, "prometheus Aura scrape query"); err != nil {
		return err
	}
	if err := c.probe(ctx, c.grafanaURL+"/api/health", "grafana"); err != nil {
		return err
	}
	if err := c.probe(ctx, c.grafanaURL+"/api/datasources/uid/aura-tempo/health", "Grafana Tempo datasource"); err != nil {
		return err
	}
	grafanaQueryURL := c.grafanaURL + "/api/datasources/proxy/uid/aura-prometheus/api/v1/query?query=" +
		url.QueryEscape(`up{job="aura"}`)
	return c.requireAuraUp(ctx, grafanaQueryURL, "Grafana Prometheus datasource")
}

func (c *SidecarChecker) requireAuraUp(ctx context.Context, queryURL, source string) error {
	body, err := c.get(ctx, queryURL)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	var response struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode %s: %w", source, err)
	}
	if response.Status != "success" {
		return fmt.Errorf("%s returned status %q", source, response.Status)
	}
	for _, result := range response.Data.Result {
		if result.Metric["job"] != "aura" || len(result.Value) < 2 {
			continue
		}
		var value string
		if err := json.Unmarshal(result.Value[1], &value); err == nil && value == "1" {
			return nil
		}
	}
	return fmt.Errorf(`%s: up{job="aura"} != 1`, source)
}

func (c *SidecarChecker) probe(ctx context.Context, endpoint, name string) error {
	if _, err := c.get(ctx, endpoint); err != nil {
		return fmt.Errorf("%s readiness: %w", name, err)
	}
	return nil
}

func (c *SidecarChecker) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, sidecarResponseLimit+1))
	if readErr != nil {
		return nil, readErr
	}
	if len(body) > sidecarResponseLimit {
		return nil, fmt.Errorf("response exceeds %d bytes", sidecarResponseLimit)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
