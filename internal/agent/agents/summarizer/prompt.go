// Package summarizer preserves the legacy payload-summarizer prompt symbol.
package summarizer

import "github.com/aura/aura/internal/agent/agentdef"

const ID = "summarizer"

var Prompt = agentdef.MustBuiltinPrompt(ID)
