package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type check struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Found   bool   `json:"found"`
	Line    int    `json:"line,omitempty"`
}

type report struct {
	Spike       string  `json:"spike"`
	AllFound    bool    `json:"all_found"`
	SourceRoot  string  `json:"source_root"`
	SourceAudit []check `json:"source_audit"`
}

func main() {
	root := os.Getenv("AURA_SPIKE_SOURCE_ROOT")
	if root == "" {
		root = `D:\tmp`
	}

	checks := []check{
		{
			Name:    "llm-graph-builder graph vector/fulltext retrieval query",
			Path:    filepath.Join(root, "llm-graph-builder", "backend", "src", "shared", "constants.py"),
			Pattern: `VECTOR_GRAPH_SEARCH_QUERY`,
		},
		{
			Name:    "llm-graph-builder vector dimension and capability check",
			Path:    filepath.Join(root, "llm-graph-builder", "backend", "src", "graphDB_dataAccess.py"),
			Pattern: `connection_check_and_get_vector_dimensions`,
		},
		{
			Name:    "llm-graph-builder duplicate entity merge cleanup",
			Path:    filepath.Join(root, "llm-graph-builder", "backend", "src", "graphDB_dataAccess.py"),
			Pattern: `merge_duplicate_nodes`,
		},
		{
			Name:    "llm-graph-builder GDS Leiden community write",
			Path:    filepath.Join(root, "llm-graph-builder", "backend", "src", "communities.py"),
			Pattern: `gds\.leiden\.write`,
		},
		{
			Name:    "mem0 OSS graph memory removal warning",
			Path:    filepath.Join(root, "mem0", "docs", "changelog", "sdk.mdx"),
			Pattern: `Graph Memory Removed \(OSS\)`,
		},
		{
			Name:    "mem0 V3 phased batch memory pipeline",
			Path:    filepath.Join(root, "mem0", "mem0", "memory", "main.py"),
			Pattern: `V3 PHASED BATCH PIPELINE`,
		},
		{
			Name:    "recursive-llm variable-backed context model",
			Path:    filepath.Join(root, "recursive-llm", "README.md"),
			Pattern: `Context is stored as a variable`,
		},
		{
			Name:    "prior Aura Neo4j speedup report",
			Path:    filepath.Join(root, "aura-neo4j-spike-2026-05-27", "REPORT.md"),
			Pattern: `15-45.*speedup`,
		},
	}

	allFound := true
	for i := range checks {
		content, err := os.ReadFile(checks[i].Path)
		if err != nil {
			allFound = false
			continue
		}
		re, err := regexp.Compile(checks[i].Pattern)
		if err != nil {
			panic(err)
		}
		lines := strings.Split(string(content), "\n")
		for lineNumber, line := range lines {
			if re.MatchString(line) {
				checks[i].Found = true
				checks[i].Line = lineNumber + 1
				break
			}
		}
		if !checks[i].Found {
			allFound = false
		}
	}

	out := report{
		Spike:       "031-phase15-memory-source-audit",
		AllFound:    allFound,
		SourceRoot:  root,
		SourceAudit: checks,
	}

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
	if !allFound {
		os.Exit(1)
	}
}
