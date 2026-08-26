package main

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

var errMissingOAuthSubject = errors.New("missing authenticated OAuth subject; each subject has its own database")

func identityFromToken(req *mcp.CallToolRequest) (string, error) {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return "", errMissingOAuthSubject
	}
	identity := strings.TrimSpace(req.Extra.TokenInfo.UserID)
	if identity == "" {
		return "", errMissingOAuthSubject
	}
	return identity, nil
}

func resolveCaller(ctx context.Context, tenants *tenants, req *mcp.CallToolRequest) (identity string, client *arcadedb.Client, err error) {
	identity, err = identityFromToken(req)
	if err != nil {
		return "", nil, err
	}
	client, err = tenants.For(ctx, identity)
	if err != nil {
		return "", nil, err
	}
	return identity, client, nil
}
