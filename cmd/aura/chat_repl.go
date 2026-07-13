// chat_repl.go holds the Runner-driven REPL loop, the inline kind-specific pause
// rendering (D-A3-02), the two-stage Ctrl+C wiring, and the list/search printers
// split out of chat.go (refactor-on-touch, ≤600 LOC). The loop drives
// runner.Runner.Turn and resumes a pause via SubmitAnswer + Turn(convID,nil).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/runner"
)

// newTracer wires the OTel TracerProvider from the AURA_OTEL_* config (flushed by
// the caller on exit). Extracted from the old runChat so the REPL boot path reuses it.
func newTracer(ctx context.Context, cfg *config.Config) (agent.TracerProvider, error) {
	v, _, _ := buildInfo()
	shutdown, err := obs.Init(ctx, obs.Config{
		Service:      "aura",
		Version:      v,
		OtelExporter: cfg.OtelExporter,
		OtelEndpoint: cfg.OtelEndpoint,
	})
	if err != nil {
		return nil, err
	}
	return obs.ShutdownFunc(shutdown), nil
}

// chatLoop is the testable REPL core. It reads a user line, drives runner.Turn,
// streams prose + the cost footer, handles a pause inline, and quits on EOF /
// `/exit` / a second consecutive Ctrl+C. History is durable (the Runner persists
// each turn) — the loop holds none in memory.
func chatLoop(ctx context.Context, d replDeps) error {
	// Session-end cleanup: auto-resolve orphan pendings + JOIN the auto-title
	// WaitGroup so a background title worker cannot outlive the REPL (Req#11,
	// goleak-clean). chatLoop owns this so every caller — production runReplOrExit
	// and the white-box tests that drive chatLoop directly — drains workers on exit.
	defer func() { _ = d.run.Stop(context.Background(), d.convID) }()

	// WR-01: ONE bufio.Reader over d.in for the whole REPL. bufio reads ahead in
	// blocks, so a second reader over the same (piped/scripted) stdin would see EOF
	// or garbage once this one buffers — the pause-answer path threads this same
	// reader rather than constructing its own.
	reader := bufio.NewReader(d.in)
	for {
		_, _ = fmt.Fprint(d.out, "› ")
		line, readErr := reader.ReadString('\n')
		line = trimLine(line)
		if dispatchCompactCommand(ctx, d.compact, d.convID, "local", false, line, d.out) {
			if readErr != nil && errors.Is(readErr, io.EOF) {
				return nil
			}
			continue
		}

		if line == exitCommand {
			_, _ = fmt.Fprintln(d.out, "bye")
			return nil
		}
		if line != "" {
			if err := runUserTurn(ctx, d, line, reader); err != nil {
				if errors.Is(err, context.Canceled) {
					_, _ = fmt.Fprintln(d.out, "\n\x1b[2m· interrotto\x1b[0m")
					continue
				}
				_, _ = fmt.Fprintln(d.errOut, "turn error:", err)
				continue
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

// runUserTurn drives one user message through the Runner, then drains any pause
// loop until the conversation is no longer paused. A first Ctrl+C aborts the turn
// (context.Canceled); a second between turns quits via the default handler.
func runUserTurn(ctx context.Context, d replDeps, userMsg string, reader *bufio.Reader) error {
	msg := userMsg
	paused, err := driveTurn(ctx, d, &msg)
	if err != nil {
		return err
	}
	// Resume loop: while the round paused, render each pending, collect the answer,
	// submit it, and continue with Turn(convID, nil) once nothing is unresolved. A
	// cancel terminates the turn (CR-01): the Runner injected the terminating tool
	// answers and auto-resolved the pendings, so the loop ends — it does NOT drive a
	// fresh Turn(nil) over the cancelled round.
	for paused {
		remaining, cancelled, rerr := renderAndAnswerPauses(ctx, d, reader)
		if rerr != nil {
			return rerr
		}
		if cancelled {
			return nil
		}
		if remaining > 0 {
			continue // more pendings to answer before the loop can continue
		}
		paused, err = driveTurn(ctx, d, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// driveTurn runs one runner.Turn, streaming prose + the cost footer, and reports
// whether the round ended in a pause (an Actions.AwaitingInput Event was seen).
func driveTurn(ctx context.Context, d replDeps, userMsg *string) (paused bool, err error) {
	turnCtx, cancel, aborted := d.newTurnCtx(ctx)
	defer cancel()

	answer, finish, usage, sawPause, runErr := renderRunnerTurn(d.out, d.run.Turn(turnCtx, d.convID, userMsg))
	_ = answer
	if runErr != nil {
		if aborted() || errors.Is(runErr, context.Canceled) {
			return false, context.Canceled
		}
		return false, runErr
	}
	if aborted() {
		return false, context.Canceled
	}
	if sawPause {
		return true, nil
	}
	// Only print the cost footer on a completed (non-paused) round.
	_, _ = fmt.Fprintln(d.out)
	_, _ = fmt.Fprintln(d.out, costFooterFromFinish(d.cfg, usage, finish))
	return false, nil
}

// renderAndAnswerPauses prints every pending pause inline (priority DESC, created_at
// ASC — the Store's ListPending order), collects the user's answer per kind, and
// submits it. It returns the remaining unresolved count after submitting and whether
// the answer was a cancel (which terminates the turn, CR-01).
func renderAndAnswerPauses(ctx context.Context, d replDeps, reader *bufio.Reader) (int, bool, error) {
	pending, err := d.run.PendingFor(ctx, d.convID)
	if err != nil {
		return 0, false, err
	}
	if len(pending) == 0 {
		return 0, false, nil
	}
	p := pending[0] // answer one at a time in FIFO order
	resp := promptForPause(d, p, reader)
	remaining, err := d.run.SubmitAnswer(ctx, p.Token, resp)
	if err != nil {
		return 0, false, err
	}
	return remaining, resp.Action == askuser.ActionCancel, nil
}

// promptForPause renders the pause inline per kind (D-A3-02) and reads the answer:
// clarification = free-text; approval = [y/N] default No; choice = numbered 1..N.
// It reads from the SHARED REPL reader (WR-01) — never a fresh bufio.Reader over the
// same stdin, which would drop buffered scripted input.
func promptForPause(d replDeps, p askuser.Pending, reader *bufio.Reader) runner.ResponseInput {
	switch p.Kind {
	case tools.KindApproval:
		_, _ = fmt.Fprintf(d.out, "\n\x1b[1m? %s\x1b[0m [y/N] ", p.Question)
		line, _ := reader.ReadString('\n')
		if isYes(trimLine(line)) {
			return runner.ResponseInput{Action: askuser.ActionAccept, Content: "yes"}
		}
		return runner.ResponseInput{Action: askuser.ActionDecline}
	case tools.KindChoice:
		opts := decodePauseOptions(p.Options)
		_, _ = fmt.Fprintf(d.out, "\n\x1b[1m? %s\x1b[0m\n", p.Question)
		for i, o := range opts {
			_, _ = fmt.Fprintf(d.out, "  %d) %s\n", i+1, o.Label)
		}
		_, _ = fmt.Fprint(d.out, "select 1-", len(opts), ": ")
		line, _ := reader.ReadString('\n')
		if idx, ok := parseChoice(trimLine(line), len(opts)); ok {
			return runner.ResponseInput{Action: askuser.ActionAccept, Content: opts[idx].Value}
		}
		return runner.ResponseInput{Action: askuser.ActionDecline}
	default: // clarification
		_, _ = fmt.Fprintf(d.out, "\n\x1b[1m? %s\x1b[0m\n› ", p.Question)
		line, _ := reader.ReadString('\n')
		return runner.ResponseInput{Action: askuser.ActionAccept, Content: trimLine(line)}
	}
}

// pauseRenderOption is the decoded {label, value} the choice renderer uses.
type pauseRenderOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// decodePauseOptions parses the paused_states.options jsonb into label/value pairs.
func decodePauseOptions(raw []byte) []pauseRenderOption {
	if len(raw) == 0 {
		return nil
	}
	var opts []pauseRenderOption
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil
	}
	return opts
}

// isYes reports whether a line is an affirmative answer (default is No).
func isYes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "s", "si", "sì":
		return true
	default:
		return false
	}
}

// parseChoice parses a 1..n selection into a 0-based index.
func parseChoice(s string, n int) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 1 || v > n {
		return 0, false
	}
	return v - 1, true
}

// trimLine strips BOM, trailing newline/CR, and surrounding spaces from a read line.
func trimLine(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
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
// the first SIGINT of this turn; aborted() reports whether it fired.
func signalTurnCtx(parent context.Context) (context.Context, context.CancelFunc, func() bool) {
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGINT)
	abortedFn := func() bool { return ctx.Err() != nil }
	return ctx, cancel, abortedFn
}

// printConversationList renders the `aura chat list` table: id, title, model,
// token totals, and the aggregated total_cost_usd (Req#9 / SC: non-zero cost).
func printConversationList(convs []conversations.Conversation) {
	if len(convs) == 0 {
		fmt.Println("ok: no conversations")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tSTATUS\tTITLE\tTOKENS\tCOST_USD")
	for _, c := range convs {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t$%.4f\n",
			c.ID, c.Status, c.DisplayTitle(), c.TotalInputTokens+c.TotalOutputTokens, c.TotalCostUSD)
	}
	_ = w.Flush()
}

// printSearchResults renders conv_id|seq|similarity|excerpt with an app-side excerpt
// window over the locked FTS query (D-A5-03).
func printSearchResults(query string, hits []conversations.SearchResult) {
	if len(hits) == 0 {
		fmt.Println("ok: no matching turns")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "CONVERSATION\tSEQ\tSIMILARITY\tEXCERPT")
	for _, h := range hits {
		_, _ = fmt.Fprintf(w, "%s\t%d\t%.3f\t%s\n",
			h.ConversationID, h.Seq, h.Similarity, excerpt(h.Content, query))
	}
	_ = w.Flush()
}

// excerpt windows the content around the first case-insensitive occurrence of query
// (±excerptWindow chars), falling back to the first excerptWindow chars on a fuzzy
// match where the literal substring is absent (D-A5-03 — pg_trgm has no ts_headline).
func excerpt(content, query string) string {
	const window = 60
	flat := strings.Join(strings.Fields(content), " ")
	idx := strings.Index(strings.ToLower(flat), strings.ToLower(query))
	if idx < 0 {
		return clampExcerpt(flat, 0, window*2)
	}
	start := idx - window
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + window
	return clampExcerpt(flat, start, end)
}

// clampExcerpt slices [start,end) of s with rune-safe bounds + ellipses.
func clampExcerpt(s string, start, end int) string {
	if end > len(s) {
		end = len(s)
	}
	if start < 0 {
		start = 0
	}
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	for end > start && end < len(s) && !utf8.RuneStart(s[end]) {
		end--
	}
	out := s[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}
