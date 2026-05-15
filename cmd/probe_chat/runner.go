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
		reply, err := sendChat(client, baseURL, token, c.Prompt)
		if err != nil {
			out = append(out, Result{
				Name:         c.Name,
				Prompt:       c.Prompt,
				TransportErr: err.Error(),
			})
			if c.Cleanup != nil {
				c.Cleanup(env)
			}
			continue
		}
		mismatches := c.Verify(reply, env)
		out = append(out, Result{
			Name:       c.Name,
			Prompt:     c.Prompt,
			Reply:      reply.Reply,
			ToolCalls:  reply.ToolCalls,
			LLMCalls:   reply.LLMCalls,
			Tokens:     reply.Tokens,
			ElapsedMs:  reply.ElapsedMs,
			Mismatches: mismatches,
			Pass:       len(mismatches) == 0,
		})
		if c.Cleanup != nil {
			c.Cleanup(env)
		}
	}
	return out
}
