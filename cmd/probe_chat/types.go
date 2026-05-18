package main

import (
	"database/sql"
	"net/http"
)

// ChatReply mirrors api.ChatReply just enough to deserialize the JSON.
type ChatReply struct {
	Reply     string `json:"reply"`
	ElapsedMs int64  `json:"elapsed_ms"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
	Tokens    int    `json:"tokens"`
}

// Case is one E2E assertion: a prompt to send and a Verify closure
// that returns one entry per assertion violation. Empty slice = PASS.
type Case struct {
	Name     string
	Category string // smoke tier: tools-files, tools-memory, tools-source, tools-web, tools-scheduler, tools-agent-note, channels-web, channels-telegram, failure-modes-phantom, failure-modes-budget, markitdown
	Prompt   string
	PromptFn func() string                            // if set, evaluated after Setup runs; overrides Prompt
	ThreadID string                                   // optional web /api/chat thread_id; empty = default thread
	Setup    func(env *Env) error                     // optional: prep state before sending
	Verify   func(reply ChatReply, env *Env) []string // required
	Cleanup  func(env *Env)                           // optional: tear down leftover state
}

// Env bundles everything a Verify function needs to consult ground truth.
type Env struct {
	DB        *sql.DB
	DBPath    string
	APIBase   string // e.g. http://localhost:18080/api
	APIToken  string
	APIClient *http.Client
}

type Result struct {
	Name         string   `json:"name"`
	Prompt       string   `json:"prompt"`
	Reply        string   `json:"reply"`
	ToolCalls    int      `json:"tool_calls"`
	LLMCalls     int      `json:"llm_calls"`
	Tokens       int      `json:"tokens"`
	ElapsedMs    int64    `json:"elapsed_ms"`
	Mismatches   []string `json:"mismatches"`
	TransportErr string   `json:"transport_err,omitempty"`
	Pass         bool     `json:"pass"`
}
