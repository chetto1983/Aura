package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	skillsCatalogAPIURL     = "https://skills.sh/api/search"
	catalogResponseMaxBytes = 1 << 20
	catalogMaxHits          = 20
)

// CatalogSearchFunc resolves one catalog query into ranked hits.
type CatalogSearchFunc func(context.Context, string) ([]CatalogHit, error)

type skillsCatalogAPIClient struct {
	client   *http.Client
	endpoint string
}

func newSkillsCatalogAPIClient(client *http.Client, endpoint string) *skillsCatalogAPIClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &skillsCatalogAPIClient{client: client, endpoint: endpoint}
}

func (c *skillsCatalogAPIClient) Search(ctx context.Context, query string) ([]CatalogHit, error) {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse skills catalog endpoint: %w", err)
	}
	values := u.Query()
	values.Set("q", query)
	u.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build skills catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("skills catalog request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("skills catalog status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, catalogResponseMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read skills catalog response: %w", err)
	}
	if len(data) > catalogResponseMaxBytes {
		return nil, fmt.Errorf("skills catalog response exceeds %d bytes", catalogResponseMaxBytes)
	}
	var envelope struct {
		Skills json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode skills catalog response: %w", err)
	}
	if len(envelope.Skills) == 0 ||
		bytes.Equal(bytes.TrimSpace(envelope.Skills), []byte("null")) {
		return nil, fmt.Errorf("skills catalog response missing skills array")
	}
	var rows []struct {
		Source   string `json:"source"`
		SkillID  string `json:"skillId"`
		Installs int64  `json:"installs"`
	}
	if err := json.Unmarshal(envelope.Skills, &rows); err != nil {
		return nil, fmt.Errorf("decode skills catalog skills: %w", err)
	}
	hits := make([]CatalogHit, 0, min(len(rows), catalogMaxHits))
	for _, row := range rows {
		source := strings.TrimSpace(row.Source)
		skill := strings.TrimSpace(row.SkillID)
		if source == "" || skill == "" {
			continue
		}
		hits = append(hits, CatalogHit{
			Source: source, Skill: skill, Installs: compactInstallCount(row.Installs),
		})
		if len(hits) == catalogMaxHits {
			break
		}
	}
	return hits, nil
}

func compactInstallCount(installs int64) string {
	if installs < 0 {
		installs = 0
	}
	switch {
	case installs >= 1_000_000:
		return compactUnit(float64(installs)/1_000_000, "M")
	case installs >= 1_000:
		return compactUnit(float64(installs)/1_000, "K")
	default:
		return strconv.FormatInt(installs, 10)
	}
}

func compactUnit(value float64, suffix string) string {
	precision := 1
	scale := 10.0
	if value >= 100 {
		precision = 0
		scale = 1
	}
	rounded := math.Round(value*scale) / scale
	return strings.TrimSuffix(
		strconv.FormatFloat(rounded, 'f', precision, 64),
		".0",
	) + suffix
}
