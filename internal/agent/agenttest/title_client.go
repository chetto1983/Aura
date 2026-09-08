package agenttest

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// TitleClient keeps independently scheduled title requests from consuming a
// task's ordered script. Each underlying fake records the requests it receives.
type TitleClient struct {
	Main  llm.Client
	Title llm.Client
}

// Stream routes independently scheduled title calls to their own script.
func (c TitleClient) Stream(ctx context.Context, request llm.Request) (<-chan llm.Chunk, error) {
	if len(request.Messages) > 0 && strings.HasPrefix(request.Messages[0].Content, "You generate a concise 4-6 word title") {
		return c.Title.Stream(ctx, request)
	}
	return c.Main.Stream(ctx, request)
}
