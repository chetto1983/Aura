package main

import (
	"strings"
	"testing"
)

const agentMemoryUpstreamRevision = "c1c2d6585fea4f2e562c17ac78d4c528e40e0f62"

func TestAgentMemoryDistributionProvenanceIsComplete(t *testing.T) {
	root := repoRootForTest(t)
	dockerfile := readProjectFile(t, root, "docker/agent-memory/Dockerfile")
	compose := readProjectFile(t, root, "compose.yaml")
	workflow := readProjectFile(t, root, ".github/workflows/ci.yml")
	license := readProjectFile(t, root, "docker/agent-memory/LICENSE")
	notice := readProjectFile(t, root, "docker/agent-memory/NOTICE")

	for _, want := range []string{
		"Apache License",
		"Version 2.0, January 2004",
		"Copyright 2024 Neo4j Labs",
	} {
		if !strings.Contains(license, want) {
			t.Errorf("Agent Memory LICENSE missing %q", want)
		}
	}
	for _, want := range []string{
		"Neo4j Labs",
		"https://github.com/neo4j-labs/agent-memory",
		"https://github.com/chetto1983/agent-memory",
		agentMemoryUpstreamRevision,
		"modified by Aura",
	} {
		if !strings.Contains(notice, want) {
			t.Errorf("Agent Memory NOTICE missing %q", want)
		}
	}

	for _, want := range []string{
		"ARG AURA_AGENT_MEMORY_VENDOR_REV",
		`RUN test -n "$AURA_AGENT_MEMORY_VENDOR_REV"`,
		`org.opencontainers.image.source="https://github.com/chetto1983/aura"`,
		`org.opencontainers.image.revision="$AURA_AGENT_MEMORY_VENDOR_REV"`,
		`org.opencontainers.image.licenses="Apache-2.0"`,
		`io.aura.agent-memory.upstream.source="https://github.com/neo4j-labs/agent-memory"`,
		`io.aura.agent-memory.upstream.fork="https://github.com/chetto1983/agent-memory"`,
		`io.aura.agent-memory.upstream.revision="` + agentMemoryUpstreamRevision + `"`,
		`COPY LICENSE NOTICE /usr/share/licenses/agent-memory/`,
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("Agent Memory Dockerfile missing %q", want)
		}
	}
	if strings.Contains(dockerfile, "ARG AURA_AGENT_MEMORY_VENDOR_REV=") {
		t.Error("Agent Memory vendor revision must not have a Dockerfile default")
	}
	if strings.Contains(dockerfile, "exact `git archive`") {
		t.Error("Agent Memory Dockerfile still claims the modified tree is an exact upstream archive")
	}

	memoryService := composeServiceBlock(t, compose, "aura-agent-memory-mcp")
	if !strings.Contains(
		memoryService,
		"AURA_AGENT_MEMORY_VENDOR_REV: ${AURA_AGENT_MEMORY_VENDOR_REV:-}",
	) {
		t.Error("Agent Memory compose build must forward the explicitly supplied vendor revision")
	}
	for _, want := range []string{
		"Build agent-memory MCP image with GHA cache",
		"build-args: |",
		"AURA_AGENT_MEMORY_VENDOR_REV=${{ github.sha }}",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("memory integration workflow missing %q", want)
		}
	}
}
