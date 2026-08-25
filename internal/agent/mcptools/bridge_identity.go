package mcptools

import (
	"context"
	"errors"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

var errMissingRemoteIdentity = errors.New("remote MCP call requires an authenticated identity")

// IdentityMetaMiddleware stamps the authenticated caller on identity-scoped
// remote MCP calls. It never consults tool arguments and has no local-operator
// fallback: without a principal, the request must not reach a tenant data plane.
func IdentityMetaMiddleware() sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			params, ok := req.GetParams().(*sdkmcp.CallToolParams)
			if !ok {
				return next(ctx, method, req)
			}
			identityID := strings.TrimSpace(identityctx.IdentityID(ctx))
			if identityID == "" {
				return nil, errMissingRemoteIdentity
			}
			mcp.SetAuraMetaField(params, mcp.MetaFieldUserIdentifier, identityID)
			return next(ctx, method, req)
		}
	}
}
