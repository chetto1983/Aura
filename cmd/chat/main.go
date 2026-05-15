// Command chat is a local CLI pipe to the running Aura instance. It POSTs
// each line from stdin to POST /api/chat and prints the assistant reply
// to stdout. Useful for stress-testing the agent stack without going
// through Telegram (no streaming, no progressive edits — one reply per
// turn) and for scripting interactions.
//
// Configuration via environment variables:
//   AURA_CHAT_URL    HTTP base URL of the Aura instance. Default: http://127.0.0.1:18080
//   AURA_CHAT_TOKEN  Bearer token (mint one via the Telegram /start command
//                    or with the request_dashboard_token LLM tool). Required.
//   AURA_CHAT_USER   Optional user_id sent in the payload. Leave blank for
//                    bearer-authenticated calls so Aura uses the token owner.
//
// Usage:
//
//   $ AURA_CHAT_TOKEN=xxx go run ./cmd/chat
//   > ciao, dimmi tre fatti su qdrant
//   ... reply ...
//   > Ctrl+D to exit
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type chatRequest struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

type chatReply struct {
	Reply     string `json:"reply"`
	ElapsedMs int64  `json:"elapsed_ms"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
	Tokens    int    `json:"tokens"`
}

type chatError struct {
	Error string `json:"error"`
}

func main() {
	var (
		baseURL = envOr("AURA_CHAT_URL", "http://127.0.0.1:18080")
		token   = strings.TrimSpace(os.Getenv("AURA_CHAT_TOKEN"))
		user    = strings.TrimSpace(os.Getenv("AURA_CHAT_USER"))
		oneShot = flag.String("m", "", "single-message mode: send this message, print the reply, and exit")
		quiet   = flag.Bool("quiet", false, "suppress the stats line under each reply")
	)
	flag.Parse()

	if token == "" {
		fmt.Fprintln(os.Stderr, "error: AURA_CHAT_TOKEN is required (mint one via the Telegram /start or /login command)")
		os.Exit(2)
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/api/chat"
	client := &http.Client{Timeout: 10 * time.Minute}

	send := func(message string) error {
		reply, err := postChat(client, endpoint, token, user, message)
		if err != nil {
			return err
		}
		fmt.Println(reply.Reply)
		if !*quiet {
			fmt.Fprintf(os.Stderr, "  [%dms · %d llm · %d tools · %d tok]\n", reply.ElapsedMs, reply.LLMCalls, reply.ToolCalls, reply.Tokens)
		}
		return nil
	}

	if msg := strings.TrimSpace(*oneShot); msg != "" {
		if err := send(msg); err != nil {
			fmt.Fprintln(os.Stderr, "chat:", err)
			os.Exit(1)
		}
		return
	}

	displayUser := user
	if displayUser == "" {
		displayUser = "(token owner)"
	}
	fmt.Fprintf(os.Stderr, "aura chat pipe → %s (user=%s) — Ctrl+D to exit\n", endpoint, displayUser)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := send(line); err != nil {
			fmt.Fprintln(os.Stderr, "chat:", err)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "stdin:", err)
		os.Exit(1)
	}
}

func postChat(client *http.Client, endpoint, token, user, message string) (chatReply, error) {
	body, err := json.Marshal(chatRequest{UserID: user, Message: message})
	if err != nil {
		return chatReply{}, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return chatReply{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return chatReply{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		var e chatError
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return chatReply{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, e.Error)
		}
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return chatReply{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	var reply chatReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return chatReply{}, fmt.Errorf("decode: %w", err)
	}
	return reply, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
