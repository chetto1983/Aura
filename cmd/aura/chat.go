// chat subcommand for `aura chat`: the interactive in-memory REPL (SPEC Req#11).
// It drives a real agent.LlmAgent over the openai_compat client, streaming each
// reply token-by-token as clean prose (the agent decodes text_response, D-13),
// printing a dim tool-activity line (D-12) and a per-turn token+USD cost footer
// (D-11). One session ThreadID is minted once (D-26); each turn mints a fresh
// RequestID. Two-stage Ctrl+C (D-10): the first SIGINT aborts the in-flight turn
// and returns to the prompt (the partial assistant message is discarded, D-29);
// a second consecutive SIGINT, EOF, or `/exit` quits cleanly. The OTel
// TracerProvider is wired from AURA_OTEL_EXPORTER and flushed on exit (Req#13).
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

// exitCommand quits the REPL cleanly when typed as a whole line (D-10).
const exitCommand = "/exit"

// chatDeps are the chat REPL's injectable dependencies so the loop is testable
// with scripted stdin + a fake client (no live OpenRouter, D-31). Production wires
// real stdin/stdout + the openai_compat client; tests wire a buffer + FakeClient.
type chatDeps struct {
	in        io.Reader
	out       io.Writer
	errOut    io.Writer
	client    llm.Client
	cfg       *config.Config
	sessionID string
	// newTurnCtx returns the per-turn context + a cancel; production wires the
	// two-stage SIGINT ctx, tests pass a plain context so no signal handler leaks.
	newTurnCtx func(parent context.Context) (context.Context, context.CancelFunc, func() bool)
}

// runChat is the `aura chat` entry point. It loads config (fail-fast on an empty
// API key with a clear stderr message + non-zero exit, NEVER a panic — Req#11),
// wires the TracerProvider (flushed on exit), builds the client, and runs the REPL.
func runChat(_ []string) {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, llm.ErrMissingAPIKey) || isMissingAPIKey(err) {
			fmt.Fprintln(os.Stderr, "aura chat: "+llm.ErrMissingAPIKey.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	tp, err := agent.NewTracerProvider(ctx, cfg.OtelExporter, cfg.OtelEndpoint)
	if err != nil {
		fmt.Fprintln(os.Stderr, "otel:", err)
		os.Exit(1)
	}
	defer func() {
		// Flush the batch on exit (Req#13). Bound the shutdown so a missing
		// collector cannot hang the process.
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = tp.Shutdown(sctx)
	}()

	sessionID, err := uuid.NewV7()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mint session id:", err)
		os.Exit(1)
	}

	deps := chatDeps{
		in:         os.Stdin,
		out:        os.Stdout,
		errOut:     os.Stderr,
		client:     openai_compat.New(cfg.LLM),
		cfg:        cfg,
		sessionID:  sessionID.String(),
		newTurnCtx: signalTurnCtx,
	}
	if err := chatLoop(ctx, deps); err != nil {
		fmt.Fprintln(os.Stderr, "aura chat:", err)
		os.Exit(1)
	}
}

// chatLoop is the testable REPL core. It keeps in-memory history across turns
// (D-26: persistence is Phase 4), mints a fresh RequestID per turn, drives
// LlmAgent.Run, streams prose + the cost footer, and quits on EOF / `/exit` / a
// second consecutive Ctrl+C. The agent owns messages[0] (the byte-stable system
// prompt) and the running history; we re-seed it with the accumulated user+
// assistant turns each turn so it sees prior context (in-memory only this phase).
func chatLoop(ctx context.Context, d chatDeps) error {
	reg := buildRegistry()
	reader := bufio.NewReader(d.in)
	var history []llm.Message // user+assistant+tool turns accumulated in memory (excludes messages[0])

	for {
		_, _ = fmt.Fprint(d.out, "› ")
		line, readErr := reader.ReadString('\n')
		line = trimLine(line)

		if line == exitCommand {
			_, _ = fmt.Fprintln(d.out, "bye")
			return nil
		}
		if line != "" {
			history = append(history, llm.Message{Role: llm.RoleUser, Content: line})
			assistantMsg, turnErr := runOneTurn(ctx, d, reg, history)
			if turnErr != nil {
				if errors.Is(turnErr, context.Canceled) {
					// First Ctrl+C: abort this turn, discard the partial assistant
					// message (D-29), return to the prompt.
					_, _ = fmt.Fprintln(d.out, "\n\x1b[2m· interrotto\x1b[0m")
					history = history[:len(history)-1] // drop the un-answered user turn
					continue
				}
				_, _ = fmt.Fprintln(d.errOut, "turn error:", turnErr)
				history = history[:len(history)-1]
				continue
			}
			if assistantMsg != "" {
				history = append(history, llm.Message{Role: llm.RoleAssistant, Content: assistantMsg})
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				_, _ = fmt.Fprintln(d.out)
				return nil
			}
			return readErr
		}
	}
}

// runOneTurn drives a single LlmAgent.Run, streaming prose to d.out and printing
// the cost footer. It returns the final assistant text (to append to history) and
// a context.Canceled error when the turn was aborted via Ctrl+C (D-10/D-29).
func runOneTurn(ctx context.Context, d chatDeps, reg *tools.Registry, history []llm.Message) (string, error) {
	turnCtx, cancel, aborted := d.newTurnCtx(ctx)
	defer cancel()

	requestID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint request id: %w", err)
	}

	la := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     d.client,
		LLM:        d.cfg.LLM,
		Registry:   reg,
		PreviewCap: d.cfg.ToolPreviewCap,
		RunDir:     d.cfg.RunDir,
		SessionID:  d.sessionID,
		UserTurns:  history,
	})
	ic := agent.InvocationContext{
		Ctx:       turnCtx,
		Agent:     la,
		RequestID: requestID,
		Branch:    "root",
		Budget:    budgetOrDefault(),
	}

	start := time.Now()
	answer, _, usage, runErr := renderTurn(d.out, func() iterSeq2 { return la.Run(ic) })
	latency := time.Since(start).Seconds()

	if runErr != nil {
		if aborted() || errors.Is(runErr, context.Canceled) {
			return "", context.Canceled
		}
		return "", runErr
	}
	if aborted() {
		return "", context.Canceled
	}

	_, _ = fmt.Fprintln(d.out)
	_, _ = fmt.Fprintln(d.out, costFooter(d.cfg.LLM.Prices, d.cfg.LLM.Model, usage, latency))
	return answer, nil
}

// budgetOrDefault builds the per-turn budget from env/defaults. A malformed env is
// surfaced by NewBudget; we fall back to a fresh default budget so a single bad
// env var never wedges the REPL (the error path already printed by config.Load).
func budgetOrDefault() *agent.Budget {
	b, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		b, _ = agent.NewBudget(agent.BudgetOptions{}) //nolint:errcheck // second call uses no overrides; cannot fail differently
	}
	return b
}

// trimLine strips the trailing newline (and CR) and surrounding spaces from a read
// line so `/exit` and prose turns match regardless of platform line endings.
func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	// Trim leading/trailing spaces only for the command match; prose keeps interior spaces.
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

// signalTurnCtx wires the two-stage Ctrl+C (D-10): the returned ctx is cancelled on
// the first SIGINT of this turn; aborted() reports whether that fired. The caller
// cancels (and thereby unregisters the handler) when the turn ends, so a second
// SIGINT between turns reaches the default handler and quits the process cleanly.
func signalTurnCtx(parent context.Context) (context.Context, context.CancelFunc, func() bool) {
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGINT)
	abortedFn := func() bool { return ctx.Err() != nil }
	return ctx, cancel, abortedFn
}
