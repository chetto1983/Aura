package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
)

type toolPipeRuntime struct {
	Registry *tools.Registry
	Context  context.Context
	Close    func() error
}

type toolPipeRuntimeFactory func(context.Context, *config.Config) (toolPipeRuntime, error)

// toolPipeSessionID is a valid, reserved UUID used only by the direct production
// runner. Conversation-aware tools may parse the session as a UUID; the old
// literal "toolpipe" made task scheduling fail before it reached the real store.
const toolPipeSessionID = "00000000-0000-0000-0000-000000000042"

type toolPipeRecord struct {
	Tool            string `json:"tool,omitempty"`
	Status          string `json:"status"`
	DurationMS      int64  `json:"duration_ms"`
	Preview         string `json:"preview,omitempty"`
	Bytes           int    `json:"bytes"`
	Truncated       bool   `json:"truncated"`
	Error           string `json:"error,omitempty"`
	Calls           int    `json:"calls"`
	TotalDurationMS int64  `json:"total_duration_ms"`
}

func runToolPipe(args []string) {
	cfg := config.LoadDB()
	err := runToolPipeCommand(
		context.Background(),
		args,
		os.Stdin,
		os.Stdout,
		cfg,
		openProductionToolPipeRuntime,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "toolpipe:", err)
		os.Exit(exitInfra)
	}
}

func openProductionToolPipeRuntime(ctx context.Context, cfg *config.Config) (toolPipeRuntime, error) {
	if cfg == nil {
		return toolPipeRuntime{}, errors.New("configuration is nil")
	}
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return toolPipeRuntime{}, fmt.Errorf("open Postgres: %w", err)
	}
	identityID, err := identityctx.OperatorIdentity(ctx, pool)
	if err != nil {
		pool.Close()
		return toolPipeRuntime{}, fmt.Errorf("resolve operator identity: %w", err)
	}
	convStore := conversations.New(pool, conversations.Config{
		RunDir:       cfg.RunDir,
		TurnCapBytes: cfg.ConversationTurnCapBytes,
	})
	taskStore := newCronTaskStore(pool, convStore)
	// nil consent: a one-shot pipe has no operator to surface an elicitation to,
	// and a nil consent keeps the mount from advertising the capability at all.
	registry, _, mcpClosers, err := buildRegistryWithMCP(
		ctx,
		cfg,
		taskStore,
		buildSandboxRouter(cfg),
		nil,
	)
	if err != nil {
		pool.Close()
		return toolPipeRuntime{}, fmt.Errorf("build production registry: %w", err)
	}
	return toolPipeRuntime{
		Registry: registry,
		Context:  identityctx.WithIdentityID(ctx, identityID),
		Close: func() error {
			// document_search holds no client to close since it became the library
			// index: it reads Postgres through the pool closed just below.
			err := closeMCPServers(mcpClosers)
			pool.Close()
			return err
		},
	}, nil
}

func runToolPipeCommand(
	ctx context.Context,
	args []string,
	in io.Reader,
	out io.Writer,
	cfg *config.Config,
	open toolPipeRuntimeFactory,
) (returnErr error) {
	manifestJSON := len(args) == 1 && args[0] == "--manifest-json"
	if len(args) != 0 && !manifestJSON {
		return errors.New("usage: aura toolpipe [--manifest-json]")
	}
	runtime, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	if runtime.Close != nil {
		defer func() {
			returnErr = errors.Join(returnErr, runtime.Close())
		}()
	}
	if runtime.Registry == nil {
		return errors.New("production registry is nil")
	}
	if runtime.Context == nil {
		runtime.Context = ctx
	}
	if manifestJSON {
		payload, err := runtime.Registry.RenderJSON()
		if err != nil {
			return fmt.Errorf("render production manifest: %w", err)
		}
		_, err = fmt.Fprintln(out, string(payload))
		return err
	}
	return executeToolPipe(runtime.Context, in, out, cfg, runtime.Registry)
}

// executeToolPipe bypasses only the model loop. Registry composition, identity,
// stores, MCP transports, routing, and tool Execute paths remain production ones.
func executeToolPipe(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	cfg *config.Config,
	registry *tools.Registry,
) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(out)

	var total time.Duration
	n := 0
	for sc.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "\ufeff")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var call struct {
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			if encodeErr := enc.Encode(toolPipeRecord{
				Status: "parse_error", Error: err.Error(),
			}); encodeErr != nil {
				return encodeErr
			}
			continue
		}
		tool, ok := registry.Get(call.Tool)
		if !ok {
			if err := enc.Encode(toolPipeRecord{
				Tool: call.Tool, Status: "unknown",
			}); err != nil {
				return err
			}
			continue
		}
		n++
		tctx := tools.WithToolCallContext(
			ctx,
			toolPipeSessionID,
			fmt.Sprintf("pipe-%d", n),
			cfg.RunDir,
			cfg.ToolPreviewCap,
		)
		start := time.Now()
		res, err := tool.Execute(tctx, []byte(call.Args))
		duration := time.Since(start)
		total += duration
		if err != nil {
			if encodeErr := enc.Encode(toolPipeRecord{
				Tool: call.Tool, Status: "error",
				DurationMS: duration.Milliseconds(), Error: err.Error(),
			}); encodeErr != nil {
				return encodeErr
			}
			continue
		}
		if err := enc.Encode(toolPipeRecord{
			Tool: call.Tool, Status: toolPipeResultStatus(res.Preview),
			DurationMS: duration.Milliseconds(), Preview: res.Preview,
			Bytes: res.Bytes, Truncated: res.Truncated,
		}); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read toolpipe input: %w", err)
	}
	return enc.Encode(toolPipeRecord{
		Status: "summary", Calls: n, TotalDurationMS: total.Milliseconds(),
	})
}

func toolPipeResultStatus(preview string) string {
	var outcome struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(preview), &outcome) == nil &&
		strings.TrimSpace(outcome.Error) != "" {
		return "rejected"
	}
	return "ok"
}
