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
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

type summary struct {
	Spike            string   `json:"spike"`
	Endpoint         string   `json:"endpoint"`
	RawToolCount     int      `json:"raw_tool_count"`
	RawTools         []string `json:"raw_tools"`
	MountedReadTools []string `json:"mounted_read_tools"`
	BlockedTools     []string `json:"blocked_tools"`
	MissingExpected  []string `json:"missing_expected"`
	AllDeferred      bool     `json:"all_deferred"`
	Verdict          string   `json:"verdict"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	endpoint := memoryEndpoint()
	logf("SETUP", "endpoint=%s", endpoint)

	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}

	cli, err := mcp.OpenServer(ctx, "agent-memory", server)
	if err != nil {
		failf("open streamable-http MCP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	logf("MCP", "initialize OK")

	if err := cli.Ping(ctx); err != nil {
		failf("ping: %v", err)
	}
	logf("MCP", "ping OK")

	defs, err := cli.ListTools(ctx)
	if err != nil {
		failf("tools/list: %v", err)
	}
	rawNames := toolNames(defs)
	logf("MCP", "tools/list returned %d tools: %s", len(rawNames), strings.Join(rawNames, ", "))

	expected := []string{
		"memory_search",
		"memory_get_context",
		"memory_store_message",
		"memory_add_entity",
		"memory_add_preference",
		"memory_add_fact",
		"memory_get_conversation",
		"memory_list_sessions",
		"memory_get_entity",
		"memory_export_graph",
		"memory_create_relationship",
		"memory_start_trace",
		"memory_record_step",
		"memory_complete_trace",
		"memory_get_observations",
		"graph_query",
	}
	missing := missingNames(rawNames, expected)

	policyServer := server
	policyServer.ToolPolicy = mcp.ManagedToolPolicy{DenyRisk: []string{mcpmanager.RiskWrite}}
	reg := tools.NewRegistry()
	closer, mounted, blocked, err := mcptools.MountManagedServerWithPolicy(ctx, reg, "memory", policyServer)
	if err != nil {
		failf("policy mount: %v", err)
	}
	defer func() { _ = closer() }()

	sort.Strings(mounted)
	blockedNames := make([]string, 0, len(blocked))
	for _, decision := range blocked {
		blockedNames = append(blockedNames, decision.ToolName)
	}
	sort.Strings(blockedNames)

	allDeferred := true
	for _, name := range mounted {
		t, ok := reg.Get(name)
		if !ok {
			failf("mounted tool %q missing from registry", name)
		}
		if !t.Spec().Deferred {
			allDeferred = false
		}
	}
	logf("POLICY", "DenyRisk=write mounted %d read-ish tools, blocked %d write-ish tools", len(mounted), len(blockedNames))

	readNeedles := []string{"memory__memory_search", "memory__memory_get_context", "memory__memory_get_conversation"}
	mountedReads := intersect(mounted, readNeedles)
	if len(missing) > 0 || !allDeferred || len(mountedReads) < 2 || len(blockedNames) == 0 {
		out := summary{
			Spike:            "032-agent-memory-mcp-live-mount",
			Endpoint:         endpoint,
			RawToolCount:     len(rawNames),
			RawTools:         rawNames,
			MountedReadTools: mountedReads,
			BlockedTools:     blockedNames,
			MissingExpected:  missing,
			AllDeferred:      allDeferred,
			Verdict:          "INVALIDATED",
		}
		printSummary(out)
		os.Exit(1)
	}

	out := summary{
		Spike:            "032-agent-memory-mcp-live-mount",
		Endpoint:         endpoint,
		RawToolCount:     len(rawNames),
		RawTools:         rawNames,
		MountedReadTools: mountedReads,
		BlockedTools:     blockedNames,
		MissingExpected:  nil,
		AllDeferred:      allDeferred,
		Verdict:          "VALIDATED",
	}
	printSummary(out)
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

func toolNames(defs []mcp.ToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return names
}

func missingNames(have, want []string) []string {
	present := make(map[string]struct{}, len(have))
	for _, name := range have {
		present[name] = struct{}{}
	}
	var missing []string
	for _, name := range want {
		if _, ok := present[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func intersect(have, want []string) []string {
	present := make(map[string]struct{}, len(have))
	for _, name := range have {
		present[name] = struct{}{}
	}
	var out []string
	for _, name := range want {
		if _, ok := present[name]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func printSummary(out summary) {
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
	logf("SUMMARY", "verdict=%s raw_tools=%d mounted_reads=%d blocked=%d", out.Verdict, out.RawToolCount, len(out.MountedReadTools), len(out.BlockedTools))
}

func logf(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}

func failf(format string, args ...any) {
	logf("ERROR", format, args...)
	os.Exit(1)
}
