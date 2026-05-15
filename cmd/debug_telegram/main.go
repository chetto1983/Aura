// debug_telegram exercises the Telegram entity-rendering pipeline without any
// live Telegram connection. Useful for verifying that Markdown → entities
// conversion produces valid output after changes to entity_markdown.go.
//
//	go run ./cmd/debug_telegram
package main

import (
	"fmt"
	"os"
	"strings"

	telegram "github.com/aura/aura/internal/telegram"
)

func main() {
	type scenario struct {
		name  string
		input string
		check func(parts []telegram.RenderedMessage) error
	}

	scenarios := []scenario{
		{
			name:  "plain text produces one part",
			input: "Hello, world!",
			check: func(parts []telegram.RenderedMessage) error {
				if len(parts) == 0 {
					return fmt.Errorf("got 0 parts, want >= 1")
				}
				if parts[0].Text != "Hello, world!" {
					return fmt.Errorf("text = %q, want %q", parts[0].Text, "Hello, world!")
				}
				return nil
			},
		},
		{
			name:  "bold markdown renders entities",
			input: "This is **bold** text.",
			check: func(parts []telegram.RenderedMessage) error {
				if len(parts) == 0 {
					return fmt.Errorf("got 0 parts, want >= 1")
				}
				if !strings.Contains(parts[0].Text, "bold") {
					return fmt.Errorf("text = %q, missing 'bold'", parts[0].Text)
				}
				return nil
			},
		},
		{
			name:  "empty string produces no parts",
			input: "",
			check: func(parts []telegram.RenderedMessage) error {
				if len(parts) != 0 {
					return fmt.Errorf("got %d parts for empty input, want 0", len(parts))
				}
				return nil
			},
		},
		{
			name:  "code block renders",
			input: "```go\nfmt.Println(\"hello\")\n```",
			check: func(parts []telegram.RenderedMessage) error {
				if len(parts) == 0 {
					return fmt.Errorf("got 0 parts, want >= 1")
				}
				if !strings.Contains(parts[0].Text, "fmt.Println") {
					return fmt.Errorf("text = %q, missing code content", parts[0].Text)
				}
				return nil
			},
		},
	}

	failed := 0
	for _, sc := range scenarios {
		parts := telegram.RenderForEntities(sc.input)
		if err := sc.check(parts); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", sc.name, err)
			failed++
		} else {
			fmt.Printf("PASS %s\n", sc.name)
		}
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d scenario(s) failed\n", failed)
		os.Exit(1)
	}
	fmt.Printf("\nall %d scenarios passed.\n", len(scenarios))
}
