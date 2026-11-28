package main

import (
	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/anthropic"
	"github.com/chetto1983/aura/internal/llm/chatgpt"
	"github.com/chetto1983/aura/internal/multimodal"
)

// primaryImageReader is the route's model reading upload images through its own client, for
// the routes the vision client's HTTP arm cannot speak: ChatGPT with each owner's account, and
// Claude with the house key. It is nil on any other route and on ChatGPT without the connection
// service; the nil is untyped, so the vision client keeps its HTTP arms.
func primaryImageReader(cfg llm.Config, plan *chatgptplan.Service, anthropicKey anthropic.KeySource) multimodal.ImageReader {
	switch {
	case cfg.Provider == llm.ChatGPTProvider && plan != nil:
		return chatgpt.New(cfg, plan)
	case cfg.Provider == llm.AnthropicProvider:
		return anthropic.New(cfg, anthropicKey)
	}
	return nil
}
