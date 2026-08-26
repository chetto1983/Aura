package mcptools

import (
	"context"
	"errors"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
)

var errMissingRemoteIdentity = errors.New("remote MCP call requires an authenticated identity")
var errRemoteIdentityMismatch = errors.New("remote MCP call identity does not own this OAuth session")

// IdentityBindingMiddleware prevents a session authenticated for one OAuth
// subject from being reused by another identity. Tenant selection stays in the
// standard bearer `sub`; nothing proprietary is written to MCP request metadata.
func IdentityBindingMiddleware(owner string) sdkmcp.Middleware {
	owner = strings.TrimSpace(owner)
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			identityID := strings.TrimSpace(identityctx.IdentityID(ctx))
			if identityID == "" {
				return nil, errMissingRemoteIdentity
			}
			if identityID != owner {
				return nil, errRemoteIdentityMismatch
			}
			return next(ctx, method, req)
		}
	}
}
