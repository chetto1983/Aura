package mcptools

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func acceptedFactEvidence(context.Context, string, map[string]any, mcp.ToolPayload) (tools.AcceptedFactEvidence, bool) {
	return tools.AcceptedFactEvidence{}, false
}
