package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

type writeResult struct {
	Variant string         `json:"variant"`
	Output  map[string]any `json:"output,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type summary struct {
	Spike          string           `json:"spike"`
	Endpoint       string           `json:"endpoint"`
	Tag            string           `json:"tag"`
	CanonicalName  string           `json:"canonical_name"`
	VariantWrites  []writeResult    `json:"variant_writes"`
	TaggedEntities []map[string]any `json:"tagged_entities"`
	ExactNameCount int              `json:"exact_name_count"`
	TaggedCount    int              `json:"tagged_count"`
	OverMerge      bool             `json:"over_merge"`
	Fragmented     bool             `json:"fragmented"`
	Verdict        string           `json:"verdict"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoint := memoryEndpoint()
	tag := fmt.Sprintf("AURA-SPIKE-034-%d", time.Now().Unix())
	canonical := "Mario Rossi " + tag
	logf("SETUP", "endpoint=%s canonical=%s", endpoint, canonical)

	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	cli, err := mcp.OpenServer(ctx, "agent-memory-chaos", server)
	if err != nil {
		failf("open MCP: %v", err)
	}
	defer func() { _ = cli.Close() }()

	variants := []string{
		canonical,
		canonical,
		canonical,
		strings.ToLower(canonical),
		"M. Rossi " + tag,
		"Mario R. " + tag,
		"Rossi Mario " + tag,
		"Dr Mario Rossi " + tag,
		"mario rossi " + tag,
		"MARlO ROSSI " + tag, // Deliberate l/I visual confusion.
	}

	results := make([]writeResult, len(variants))
	var wg sync.WaitGroup
	for i, variant := range variants {
		wg.Add(1)
		go func(i int, variant string) {
			defer wg.Done()
			args := map[string]any{
				"name":        variant,
				"entity_type": "PERSON",
				"description": "Synthetic Mario Rossi chaos entity for " + tag + "; canonical target " + canonical,
				"aliases":     []string{canonical, "Mario Rossi", "M. Rossi " + tag},
				"metadata":    map[string]any{"spike": "034", "tag": tag, "canonical": canonical},
			}
			text, err := cli.CallTool(ctx, "memory_add_entity", args)
			results[i] = writeResult{Variant: variant}
			if err != nil {
				results[i].Error = err.Error()
				return
			}
			var decoded map[string]any
			if json.Unmarshal([]byte(text), &decoded) != nil {
				results[i].Error = "non-json result: " + trim(text)
				return
			}
			if errVal, ok := decoded["error"]; ok && fmt.Sprint(errVal) != "" {
				results[i].Error = fmt.Sprint(errVal)
				return
			}
			results[i].Output = decoded
		}(i, variant)
	}
	wg.Wait()

	for _, result := range results {
		if result.Error != "" {
			logf("WRITE", "%s error=%s", result.Variant, result.Error)
		} else {
			logf("WRITE", "%s ok", result.Variant)
		}
	}

	taggedRows, err := graphRows(ctx, cli, `
MATCH (e:Entity)
WHERE toString(e.name) CONTAINS $tag
   OR toString(e.canonical_name) CONTAINS $tag
   OR toString(e.description) CONTAINS $tag
RETURN e.id AS id, e.name AS name, e.canonical_name AS canonical_name, e.description AS description
ORDER BY name
LIMIT 50`, map[string]any{"tag": tag})
	if err != nil {
		failf("tagged entity query: %v", err)
	}

	exactRows, err := graphRows(ctx, cli, `
MATCH (e:Entity {name: $name})
RETURN e.id AS id, e.name AS name
LIMIT 50`, map[string]any{"name": canonical})
	if err != nil {
		failf("exact entity query: %v", err)
	}

	overMerge := false
	for _, result := range results {
		if result.Error != "" || result.Output == nil {
			continue
		}
		if dedup, ok := result.Output["deduplication"].(map[string]any); ok {
			matched, _ := dedup["matched_entity_name"].(string)
			matched = strings.TrimSpace(matched)
			if matched != "" && !strings.Contains(matched, tag) {
				overMerge = true
			}
		}
	}

	writeErrors := 0
	for _, result := range results {
		if result.Error != "" {
			writeErrors++
		}
	}
	fragmented := len(taggedRows) > 1
	verdict := "VALIDATED"
	if writeErrors > 0 || len(exactRows) != 1 || overMerge || fragmented {
		verdict = "INVALIDATED"
	}

	out := summary{
		Spike:          "034-agent-memory-dedup-chaos",
		Endpoint:       endpoint,
		Tag:            tag,
		CanonicalName:  canonical,
		VariantWrites:  results,
		TaggedEntities: taggedRows,
		ExactNameCount: len(exactRows),
		TaggedCount:    len(taggedRows),
		OverMerge:      overMerge,
		Fragmented:     fragmented,
		Verdict:        verdict,
	}
	printSummary(out)
	if verdict != "VALIDATED" {
		os.Exit(1)
	}
}

func graphRows(ctx context.Context, cli mcp.Transport, query string, params map[string]any) ([]map[string]any, error) {
	text, err := cli.CallTool(ctx, "graph_query", map[string]any{"query": query, "parameters": params})
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Success bool             `json:"success"`
		Rows    []map[string]any `json:"rows"`
		Error   string           `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, err
	}
	if decoded.Error != "" {
		return nil, fmt.Errorf("%s", decoded.Error)
	}
	return decoded.Rows, nil
}

func memoryEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL")); v != "" {
		return v
	}
	port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT"))
	if port == "" {
		port = "8091"
	}
	return "http://127.0.0.1:" + port + "/mcp/"
}

func printSummary(out summary) {
	sort.Slice(out.TaggedEntities, func(i, j int) bool {
		return fmt.Sprint(out.TaggedEntities[i]["name"]) < fmt.Sprint(out.TaggedEntities[j]["name"])
	})
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
	logf("SUMMARY", "verdict=%s exact=%d tagged=%d over_merge=%v fragmented=%v", out.Verdict, out.ExactNameCount, out.TaggedCount, out.OverMerge, out.Fragmented)
}

func trim(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "...[truncated]"
	}
	return s
}

func logf(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}

func failf(format string, args ...any) {
	logf("ERROR", format, args...)
	os.Exit(1)
}
