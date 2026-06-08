package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

type check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

type summary struct {
	Spike     string  `json:"spike"`
	Endpoint  string  `json:"endpoint"`
	SessionID string  `json:"session_id"`
	Tag       string  `json:"tag"`
	Checks    []check `json:"checks"`
	Verdict   string  `json:"verdict"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoint := memoryEndpoint()
	tag := fmt.Sprintf("AURA-SPIKE-033-%d", time.Now().Unix())
	sessionID := strings.ToLower(tag) + "-session"
	logf("SETUP", "endpoint=%s session=%s tag=%s", endpoint, sessionID, tag)

	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	reg := tools.NewRegistry()
	closer, mounted, err := mcptools.MountManagedServer(ctx, reg, "memory", server)
	if err != nil {
		failf("mount memory MCP: %v", err)
	}
	defer func() { _ = closer() }()
	sort.Strings(mounted)
	logf("MOUNT", "mounted=%d", len(mounted))

	direct, err := mcp.OpenServer(ctx, "agent-memory-direct", server)
	if err != nil {
		failf("open direct MCP: %v", err)
	}
	defer func() { _ = direct.Close() }()

	message := tag + " Davide prefers limone gelato while reviewing robot manuals."
	entityName := "RoboManual " + tag
	preference := "Use compact verification notes for " + tag
	factSubject := tag

	var checks []check
	checks = append(checks, mustStore(ctx, reg, sessionID, "memory__memory_store_message", map[string]any{
		"content":    message,
		"role":       "user",
		"session_id": sessionID,
		"metadata":   map[string]any{"spike": "033", "tag": tag},
	}, "store_message"))
	checks = append(checks, mustStore(ctx, reg, sessionID, "memory__memory_add_entity", map[string]any{
		"name":        entityName,
		"entity_type": "OBJECT",
		"description": "Synthetic spike entity for " + tag,
		"aliases":     []string{"robot manual " + tag},
		"metadata":    map[string]any{"spike": "033", "tag": tag},
	}, "add_entity"))
	checks = append(checks, mustStore(ctx, reg, sessionID, "memory__memory_add_preference", map[string]any{
		"category":   "spike_033",
		"preference": preference,
		"context":    "Stored by " + tag,
		"confidence": 1.0,
	}, "add_preference"))
	checks = append(checks, mustStore(ctx, reg, sessionID, "memory__memory_add_fact", map[string]any{
		"subject":      factSubject,
		"predicate":    "validates",
		"object_value": "agent-memory MCP write-read ground truth",
		"confidence":   1.0,
		"metadata":     map[string]any{"spike": "033", "tag": tag},
	}, "add_fact"))

	conversation, convErr := execute(ctx, reg, sessionID, "memory__memory_get_conversation", map[string]any{
		"session_id":       sessionID,
		"limit":            20,
		"include_metadata": true,
	})
	checks = append(checks, check{
		Name:   "conversation_readback",
		Pass:   convErr == nil && strings.Contains(conversation, message),
		Detail: detailOrError(conversation, convErr),
	})

	searchMessages, msgErr := execute(ctx, reg, sessionID, "memory__memory_search", map[string]any{
		"query":        tag,
		"limit":        5,
		"memory_types": []string{"messages"},
		"session_id":   sessionID,
		"threshold":    0.0,
	})
	checks = append(checks, check{
		Name:   "message_search_readback",
		Pass:   msgErr == nil && strings.Contains(searchMessages, message),
		Detail: detailOrError(searchMessages, msgErr),
	})

	searchEntity, entityErr := execute(ctx, reg, sessionID, "memory__memory_search", map[string]any{
		"query":        entityName,
		"limit":        5,
		"memory_types": []string{"entities"},
		"threshold":    0.0,
	})
	checks = append(checks, check{
		Name:   "entity_search_readback",
		Pass:   entityErr == nil && strings.Contains(searchEntity, "Synthetic spike entity for "+tag),
		Detail: detailOrError(searchEntity, entityErr),
	})

	searchPreference, prefErr := execute(ctx, reg, sessionID, "memory__memory_search", map[string]any{
		"query":        preference,
		"limit":        5,
		"memory_types": []string{"preferences"},
		"threshold":    0.0,
	})
	checks = append(checks, check{
		Name:   "preference_search_readback",
		Pass:   prefErr == nil && strings.Contains(searchPreference, preference),
		Detail: detailOrError(searchPreference, prefErr),
	})

	contextText, contextErr := execute(ctx, reg, sessionID, "memory__memory_get_context", map[string]any{
		"session_id":         sessionID,
		"query":              tag,
		"max_items":          5,
		"include_short_term": true,
		"include_long_term":  true,
		"include_reasoning":  false,
	})
	checks = append(checks, check{
		Name:   "context_readback",
		Pass:   contextErr == nil && strings.Contains(contextText, tag),
		Detail: detailOrError(contextText, contextErr),
	})

	factResult, factErr := direct.CallTool(ctx, "graph_query", map[string]any{
		"query": "MATCH (f:Fact {subject: $subject}) RETURN f.subject AS subject, f.predicate AS predicate, f.object AS object LIMIT 5",
		"parameters": map[string]any{
			"subject": factSubject,
		},
	})
	checks = append(checks, check{
		Name:   "fact_graph_readback",
		Pass:   factErr == nil && strings.Contains(factResult, factSubject),
		Detail: detailOrError(factResult, factErr),
	})

	verdict := "VALIDATED"
	for _, c := range checks {
		logf("CHECK", "%s pass=%v", c.Name, c.Pass)
		if !c.Pass {
			verdict = "INVALIDATED"
		}
	}
	out := summary{Spike: "033-agent-memory-write-read-ground-truth", Endpoint: endpoint, SessionID: sessionID, Tag: tag, Checks: checks, Verdict: verdict}
	printSummary(out)
	if verdict != "VALIDATED" {
		os.Exit(1)
	}
}

func mustStore(ctx context.Context, reg *tools.Registry, sessionID, toolName string, args map[string]any, name string) check {
	out, err := execute(ctx, reg, sessionID, toolName, args)
	pass := err == nil && strings.Contains(out, `"stored": true`)
	if !pass && err == nil {
		pass = strings.Contains(out, `"stored":true`)
	}
	return check{Name: name, Pass: pass, Detail: detailOrError(out, err)}
}

func execute(ctx context.Context, reg *tools.Registry, sessionID, toolName string, args map[string]any) (string, error) {
	t, ok := reg.Get(toolName)
	if !ok {
		return "", fmt.Errorf("tool %s not mounted", toolName)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	callID := strings.ReplaceAll(toolName, "__", "-") + fmt.Sprintf("-%d", time.Now().UnixNano())
	toolCtx := tools.WithToolCallContext(ctx, sessionID, callID, os.TempDir(), 1<<20)
	res, err := t.Execute(toolCtx, raw)
	if err != nil {
		return res.Preview, err
	}
	if strings.HasPrefix(res.Preview, "error:") {
		return res.Preview, fmt.Errorf("%s", res.Preview)
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(res.Preview), &decoded) == nil {
		if errVal, ok := decoded["error"]; ok && fmt.Sprint(errVal) != "" {
			return res.Preview, fmt.Errorf("%v", errVal)
		}
	}
	return res.Preview, nil
}

func detailOrError(s string, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 500 {
		return s[:500] + "...[truncated]"
	}
	return s
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
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
	logf("SUMMARY", "verdict=%s tag=%s checks=%d", out.Verdict, out.Tag, len(out.Checks))
}

func logf(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}

func failf(format string, args ...any) {
	logf("ERROR", format, args...)
	os.Exit(1)
}
