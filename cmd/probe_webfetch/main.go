// One-shot: dump WebTool fetch output verbatim for an URL.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aura/aura/internal/tools"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe_webfetch <url>")
		os.Exit(2)
	}
	tool := tools.NewWebTool("")
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "fetch",
		"url":    os.Args[1],
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(1)
	}
	fmt.Println("=== BYTES:", len(out), "===")
	fmt.Println(out)
}
