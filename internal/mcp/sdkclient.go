package mcp

import (
	"context"
	"errors"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpClientName    = "aura"
	mcpClientVersion = "2.1.0"
)

var errSDKClientNotImplemented = errors.New("sdk client not implemented")

// SessionOptions is the seam later plans fill: sending middleware (45.1-05's identity
// stamping), the tool-list-changed notification, and elicitation (45.1-07).
type SessionOptions struct {
	Logger          *slog.Logger
	Sending         []sdkmcp.Middleware
	ToolListChanged func(context.Context, *sdkmcp.ToolListChangedRequest)
	Elicitation     func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error)
}

// SDKClientOptions builds the client options every Aura MCP session is opened with.
func SDKClientOptions(_ SessionOptions) *sdkmcp.ClientOptions {
	return nil
}

func newSDKClient(o SessionOptions) *sdkmcp.Client {
	return sdkmcp.NewClient(&sdkmcp.Implementation{Name: mcpClientName, Version: mcpClientVersion}, SDKClientOptions(o))
}

// OpenSDKSession opens a live session to one managed MCP server over whichever
// transport Classify resolves, with Aura's network policy attached.
func OpenSDKSession(_ context.Context, name string, _ ManagedServer, _ EgressPolicy, _ SessionOptions) (*sdkmcp.ClientSession, error) {
	return nil, TransportErrorf(name, errSDKClientNotImplemented)
}

// OpenSDKSessionForConfig opens a stdio session for a declared launch config.
func OpenSDKSessionForConfig(_, _ context.Context, name string, _ ServerConfig, _ SessionOptions) (*sdkmcp.ClientSession, error) {
	return nil, TransportErrorf(name, errSDKClientNotImplemented)
}
