//go:build calendar_integration || pim_mail_e2e

package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const curatedCalendarToolName = "calendar"

func callCalendar(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, args map[string]any) (text string, isError bool) {
	t.Helper()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: curatedCalendarToolName, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", curatedCalendarToolName, err)
	}
	return DecodeToolResult(res)
}
