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
	"github.com/google/uuid"
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

type toolPipeLargeResult struct{}

func (toolPipeLargeResult) Spec() tools.Spec {
	return tools.Spec{
		Name:        "large",
		Summary:     "large",
		Description: "large",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (toolPipeLargeResult) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	preview := "start-" + strings.Repeat("x", 640) + "-end"
	return tools.ToolResult{Preview: preview, Bytes: len(preview)}, nil
}

type toolPipeRejectedResult struct{}

func (toolPipeRejectedResult) Spec() tools.Spec {
	return tools.Spec{
		Name:        "rejected",
		Summary:     "rejected",
		Description: "rejected",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (toolPipeRejectedResult) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	const preview = `{"error":"timeout","message":"fetch timed out"}`
	return tools.ToolResult{Preview: preview, Bytes: len(preview)}, nil
}

type toolPipeTaskStore struct {
	originConversationID string
}

func (s *toolPipeTaskStore) CreateScheduledTask(
	_ context.Context,
	in tools.CreateTaskInput,
) (tools.ScheduledTask, error) {
	s.originConversationID = in.OriginConversationID
	return tools.ScheduledTask{
		ID: "00000000-0000-0000-0000-000000000001",
	}, nil
}

func (*toolPipeTaskStore) ListScheduledTasks(context.Context) ([]tools.ScheduledTask, error) {
	return nil, nil
}

func (*toolPipeTaskStore) CancelScheduledTask(context.Context, string) error {
	return nil
}

func (*toolPipeTaskStore) RunScheduledTaskNow(context.Context, string) error {
	return nil
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
	if got := output.String(); !strings.Contains(got, `"tool":"echo"`) ||
		!strings.Contains(got, `"status":"ok"`) ||
		strings.Contains(got, `"status":"parse_error"`) {
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

func TestRunToolPipeCommandEmitsCompleteBoundedPreviewAndMetrics(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(toolPipeLargeResult{})
	var output bytes.Buffer
	err := runToolPipeCommand(
		t.Context(),
		nil,
		strings.NewReader(`{"tool":"large","args":{}}`+"\n"),
		&output,
		&config.Config{ToolPreviewCap: 30_000},
		func(context.Context, *config.Config) (toolPipeRuntime, error) {
			return toolPipeRuntime{Registry: registry, Context: t.Context()}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "-end") {
		t.Fatalf("direct runner applied a second preview cap: %q", output.String())
	}
	for _, field := range []string{`"status":"ok"`, `"bytes":650`, `"truncated":false`, `"duration_ms":`} {
		if !strings.Contains(output.String(), field) {
			t.Fatalf("direct runner output missing %s: %q", field, output.String())
		}
	}
}

func TestRunToolPipeCommandDistinguishesModelVisibleRejection(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(toolPipeRejectedResult{})
	var output bytes.Buffer
	err := executeToolPipe(
		t.Context(),
		strings.NewReader(`{"tool":"rejected","args":{}}`+"\n"),
		&output,
		&config.Config{ToolPreviewCap: 30_000},
		registry,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status":"rejected"`) {
		t.Fatalf("model-visible rejection rendered as success: %q", output.String())
	}
}

func TestExecuteToolPipeUsesValidSyntheticConversationID(t *testing.T) {
	store := &toolPipeTaskStore{}
	registry := tools.NewRegistry()
	registry.Register(&tools.TaskTool{Store: store})
	var output bytes.Buffer
	err := executeToolPipe(
		t.Context(),
		strings.NewReader(
			`{"tool":"task","args":{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"audit"},"notify":"stdout"}}`+"\n",
		),
		&output,
		&config.Config{ToolPreviewCap: 30_000},
		registry,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(store.originConversationID); err != nil {
		t.Fatalf(
			"toolpipe origin conversation ID = %q, want valid UUID: %v",
			store.originConversationID,
			err,
		)
	}
}
