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
// standard bearer `sub`; nothing is written to MCP request metadata (_meta) --
// that stays true after D-10 (Phase 51). What changed: a host-derived actor
// (run id + writer role, mcpActor in bridge_actor.go) now rides as an HTTP
// header on the CONNECTION -- below _meta, at the transport layer -- so
// cmd/arcadedb-mcp can read who is really writing a memory fact beside `sub`,
// without the model ever asserting it and without touching JSON-RPC request
// metadata at all. _meta stays exactly as untouched as this comment always
// said; the header is a different wire, not a loophole in this one.
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
