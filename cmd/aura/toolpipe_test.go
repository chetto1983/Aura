package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
)

type toolPipeEcho struct{}

func (toolPipeEcho) Spec() tools.Spec {
	return tools.Spec{
		Name:        "echo",
		Summary:     "echo",
		Description: "echo",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (toolPipeEcho) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, string(args))
}

func TestRunToolPipeCommandUsesRuntimeAndCloses(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(toolPipeEcho{})
	closed := false
	open := func(context.Context, *config.Config) (toolPipeRuntime, error) {
		return toolPipeRuntime{
			Registry: registry,
			Context:  context.Background(),
			Close: func() error {
				closed = true
				return nil
			},
		}, nil
	}
	input := strings.NewReader("\ufeff{\"tool\":\"echo\",\"args\":{\"value\":\"ok\"}}\n")
	var output bytes.Buffer
	err := runToolPipeCommand(t.Context(), nil, input, &output, &config.Config{}, open)
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("runtime was not closed")
	}
	if got := output.String(); !strings.Contains(got, "[echo] OK") || strings.Contains(got, "parse error") {
		t.Fatalf("output = %q", got)
	}
}

func TestRunToolPipeCommandRendersProductionManifest(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(toolPipeEcho{})
	var output bytes.Buffer
	err := runToolPipeCommand(
		t.Context(),
		[]string{"--manifest-json"},
		strings.NewReader(""),
		&output,
		&config.Config{},
		func(context.Context, *config.Config) (toolPipeRuntime, error) {
			return toolPipeRuntime{Registry: registry, Context: t.Context()}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var manifest []tools.ManifestEntryJSON
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest JSON: %v\n%s", err, output.String())
	}
	if len(manifest) != 1 || manifest[0].Name != "echo" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestRunToolPipeCommandReturnsOpenAndCloseErrors(t *testing.T) {
	openErr := errors.New("open failed")
	cfg := &config.Config{}
	if err := runToolPipeCommand(
		t.Context(),
		nil,
		strings.NewReader(""),
		&bytes.Buffer{},
		cfg,
		func(context.Context, *config.Config) (toolPipeRuntime, error) {
			return toolPipeRuntime{}, openErr
		},
	); !errors.Is(err, openErr) {
		t.Fatalf("open err = %v", err)
	}

	closeErr := errors.New("close failed")
	err := runToolPipeCommand(
		t.Context(),
		nil,
		strings.NewReader(""),
		&bytes.Buffer{},
		cfg,
		func(context.Context, *config.Config) (toolPipeRuntime, error) {
			return toolPipeRuntime{Registry: tools.NewRegistry(), Context: context.Background(), Close: func() error {
				return closeErr
			}}, nil
		},
	)
	if !errors.Is(err, closeErr) {
		t.Fatalf("close err = %v", err)
	}
}
