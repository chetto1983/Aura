package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// =========================================================================
// REPORT
// =========================================================================

func printOneShot(r ChatReply, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	fmt.Printf("reply:      %s\n", r.Reply)
	fmt.Printf("tool_calls: %d\n", r.ToolCalls)
	fmt.Printf("llm_calls:  %d\n", r.LLMCalls)
	fmt.Printf("tokens:     %d\n", r.Tokens)
	fmt.Printf("elapsed_ms: %d\n", r.ElapsedMs)
}

func printReport(results []Result) {
	passed, failed := 0, 0
	for _, r := range results {
		if r.Pass {
			passed++
		} else {
			failed++
		}
	}
	fmt.Printf("=== probe_chat: %d total, %d PASS, %d FAIL ===\n\n", len(results), passed, failed)
	for _, r := range results {
		status := "PASS"
		if !r.Pass {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s  (tool_calls=%d, llm=%d, tokens=%d, elapsed=%dms)\n",
			status, r.Name, r.ToolCalls, r.LLMCalls, r.Tokens, r.ElapsedMs)
		fmt.Printf("  prompt: %s\n", truncate(r.Prompt, 160))
		fmt.Printf("  reply : %s\n", truncate(r.Reply, 280))
		if r.TransportErr != "" {
			fmt.Printf("  TRANSPORT ERROR: %s\n", r.TransportErr)
		}
		for _, m := range r.Mismatches {
			fmt.Printf("  MISMATCH: %s\n", m)
		}
		fmt.Println()
	}
}

func anyFailed(results []Result) bool {
	for _, r := range results {
		if !r.Pass {
			return true
		}
	}
	return false
}
