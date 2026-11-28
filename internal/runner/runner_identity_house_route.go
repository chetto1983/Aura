package runner

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

// identityScopedLLMClient is a house route's one shared client (the ChatGPT plan, the
// Claude house key) streaming as one identity, so each turn is attributed to its owner.
type identityScopedLLMClient struct {
	client     llm.Client
	identityID string
}

func (c *identityScopedLLMClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if c.client == nil {
		return nil, errors.New("route client is unavailable")
	}
	return c.client.Stream(identityctx.WithIdentityID(ctx, c.identityID), req)
}
