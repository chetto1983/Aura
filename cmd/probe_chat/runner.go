package main

import (
	"fmt"
	"net/http"
)

// =========================================================================
// EXECUTION
// =========================================================================

func runAll(client *http.Client, baseURL, token string, env *Env, cases []Case) []Result {
	out := make([]Result, 0, len(cases))
	for _, c := range cases {
		if c.Setup != nil {
			if err := c.Setup(env); err != nil {
				out = append(out, Result{
					Name:         c.Name,
					Prompt:       c.Prompt,
					TransportErr: fmt.Sprintf("setup: %v", err),
				})
				continue
			}
		}
		prompt := c.Prompt
		if c.PromptFn != nil {
			prompt = c.PromptFn()
		}
		reply, err := sendChatWithThread(client, baseURL, token, prompt, c.ThreadID)
		if err != nil {
			out = append(out, Result{
				Name:         c.Name,
				Prompt:       prompt,
				TransportErr: err.Error(),
			})
			if c.Cleanup != nil {
				c.Cleanup(env)
			}
			continue
		}
		mismatches := c.Verify(reply, env)
		var metrics map[string]any
		if c.Metrics != nil {
			metrics = c.Metrics(reply, env)
		}
		out = append(out, Result{
			Name:       c.Name,
			Prompt:     prompt,
			Reply:      reply.Reply,
			ToolCalls:  reply.ToolCalls,
			LLMCalls:   reply.LLMCalls,
			Tokens:     reply.Tokens,
			ElapsedMs:  reply.ElapsedMs,
			Metrics:    metrics,
			Mismatches: mismatches,
			Pass:       len(mismatches) == 0,
		})
		if c.Cleanup != nil {
			c.Cleanup(env)
		}
	}
	return out
}
