// Aura entry point. Sub-commands:
//
//	aura serve       — run the long-lived agent runtime (default in production)
//	aura shell       — interactive REPL against the agent loop
//	aura chat <msg>  — one-shot turn, prints the assistant reply, exits
//	aura tools       — print the tool manifest (active + deferred)
//
// Tabula-rasa scaffold: only `tools` and `chat` are wired; the rest print a
// TODO marker so the entry is build-clean while the four components fill in.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "tools":
		printTools()
	case "chat":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aura chat <message>")
			os.Exit(1)
		}
		chatOnce(os.Args[2])
	case "db":
		runDB(os.Args[2:])
	case "shell", "serve":
		fmt.Println("TODO: implemented by the agent-loop and CLI slices")
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: aura {serve|shell|chat <msg>|tools|db <sub>}")
}

func buildRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	return reg
}

func printTools() {
	reg := buildRegistry()
	fmt.Print(reg.RenderText())
}

func chatOnce(msg string) {
	reg := buildRegistry()
	loop := agent.NewLoop(stubClient{}, "tabula-rasa-stub", "You are Aura. Respond concisely using text_response.", reg)
	reply, err := loop.Turn(context.Background(), msg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(reply)
}

// stubClient returns a canned text_response so `aura chat hello` is runnable
// before the OpenAI-compat client slice lands. Replaced in the agent-loop
// slice with a real llm.Client.
type stubClient struct{}

func (stubClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 2)
	ch <- llm.Chunk{
		ToolCall: &llm.ToolCall{
			ID:   "stub-1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "text_response", Arguments: `{"text":"hello from tabula-rasa stub — agent loop is wired, llm client is the next slice"}`},
		},
	}
	close(ch)
	return ch, nil
}
