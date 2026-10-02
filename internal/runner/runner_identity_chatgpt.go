package runner

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type identityScopedLLMClient struct {
	client     llm.Client
	identityID string
}

func (c *identityScopedLLMClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if c.client == nil {
		return nil, errors.New("ChatGPT plan client is unavailable")
	}
	return c.client.Stream(identityctx.WithIdentityID(ctx, c.identityID), req)
}
