package main

import (
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/runner"
)

// identityLLMResolver returns the daemon's one per-identity LLM resolver (CRED-01/CRED-05/
// CRED-07/D-11), built on first use. Every path that resolves an identity's client shares it:
// the runner, Telegram, cron's agent_job, the delegation loop, and the credit API, whose
// Invalidate after a cap change clears only the instance it holds. With one resolver per
// caller, a topped-up member kept the runner's cached refusal (measured 2026-09-11).
//
// A nil pool or an unset AURA_AUTHULA_SECRET disables it and every caller degrades to its
// Runtime/Client fallback; a store construction error is logged and also degrades, never
// panics a boot over a malformed secret. Callers compare the returned concrete pointer to nil
// before assigning it to an interface field, so a disabled resolver never becomes a non-nil
// interface (#2924).
func identityLLMResolver(chat *chatEnv) *runner.IdentityLLMResolver {
	if chat == nil {
		return nil
	}
	chat.identityLLMOnce.Do(func() {
		chat.identityLLM = newIdentityLLMResolver(chat)
	})
	return chat.identityLLM
}

func newIdentityLLMResolver(chat *chatEnv) *runner.IdentityLLMResolver {
	if chat.pool == nil || chat.cfg == nil || strings.TrimSpace(chat.cfg.AuthulaSecret) == "" {
		return nil
	}
	store, err := identitykey.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("identity_llm_resolver.store_unavailable", "error", err)
		return nil
	}
	return runner.NewIdentityLLMResolver(store, chat.llmRuntime, chat.cfg.LLM, nil, creditExhaustedClient{})
}
