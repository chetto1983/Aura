package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/chatgpt"
	"github.com/chetto1983/aura/internal/multimodal"
)

// chatGPTRoute is the ChatGPT reader for the document's owner, and the context naming that
// owner, when the chat runs on ChatGPT; off ChatGPT it is no reader at all.
//
// The owner is the ingest child's identity, and the tokens come from the store the daemon
// writes, opened and never created. A missing identity or store is an installation mistake,
// so it fails and the file is retried, instead of reading as an owner who never connected
// and ending card-only for good.
func chatGPTRoute(ctx context.Context, cfg *config.Config, getenv func(string) string) (context.Context, multimodal.ImageReader, error) {
	if cfg.LLM.Provider != llm.ChatGPTProvider {
		return ctx, nil, nil
	}
	identity := strings.TrimSpace(getenv("AURA_INGEST_IDENTITY_ID"))
	if identity == "" {
		return ctx, nil, errors.New("a ChatGPT route reads images with the document owner's account: AURA_INGEST_IDENTITY_ID is not set")
	}
	plan, err := chatgptplan.Open(cfg.ChatGPTPlanRoot(), cfg.AuthulaSecret)
	if err != nil {
		return ctx, nil, fmt.Errorf("open the ChatGPT credentials under %s: %w", cfg.ChatGPTPlanRoot(), err)
	}
	return identityctx.WithIdentityID(ctx, identity), chatgpt.New(cfg.LLM, plan), nil
}
