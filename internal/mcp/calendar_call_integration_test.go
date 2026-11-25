//go:build calendar_integration || pim_mail_e2e

package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func callPIM(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, tool string, args map[string]any) (text string, isError bool) {
	t.Helper()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", tool, err)
	}
	return DecodeToolResult(res)
}
