package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// TestChat_ToolUsingTurn drives a full end-to-end tool turn through chatLoop: the
// agent calls current_time (a non-terminal tool -> dim activity line + a skipped
// tool-result preview), then text_response. The REPL output shows the activity
// line and clean prose, never the raw tool output.
func TestChat_ToolUsingTurn(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "current_time", `{}`)),
		textResponseTurn("c2", "Sono le 14:24 UTC.", llm.Usage{PromptTokens: 50, CompletionTokens: 12}),
	)
	d, out, _ := testChatDeps(t, "che ore sono\n", fc)
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Sono le 14:24 UTC.") {
		t.Fatalf("final prose not rendered:\n%s", got)
	}
	if !strings.Contains(got, "· current_time") {
		t.Fatalf("dim tool-activity line missing:\n%s", got)
	}
	// The raw RFC3339 timestamp the tool produced must not leak into the prose.
	if strings.Contains(got, "T") && strings.Contains(got, "Z\n") {
		// crude check: an RFC3339-ish blob would carry a 'T' + 'Z'; assert no bare timestamp line
		for _, line := range strings.Split(got, "\n") {
			if strings.HasSuffix(line, "Z") && strings.Count(line, ":") >= 2 && !strings.Contains(line, "·") {
				t.Fatalf("raw tool timestamp leaked into prose: %q", line)
			}
		}
	}
}

// TestChat_BudgetTrip drives a turn that trips the step budget: the agent keeps
// calling a non-terminal tool, the budget terminal Event fires, and chatLoop
// returns to the prompt without crashing. The terminal Event carries no prose, so
// no assistant message is appended.
func TestChat_BudgetTrip(t *testing.T) {
	turns := make([]agenttest.FakeTurn, 0, 10)
	for i := 0; i < 10; i++ {
		turns = append(turns, agenttest.ToolCallTurn(
			agenttest.MakeToolCall("c", "current_time", `{"i":`+itoa(i)+`}`)))
	}
	fc := agenttest.NewFakeClient(turns...)
	// Trip the step budget at 2 via the env budgetOrDefault reads.
	t.Setenv("AURA_LOOP_MAX_STEPS", "2")
	d, out, _ := testChatDeps(t, "vai\n", fc)
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	// A cost footer still renders for the (terminated) turn.
	if !strings.Contains(out.String(), " tok (") {
		t.Fatalf("no cost footer rendered after a budget trip:\n%s", out.String())
	}
}

// TestChat_TurnError asserts a real infra Stream error is surfaced to errOut and
// the un-answered user turn is dropped (chatLoop continues to EOF cleanly).
func TestChat_TurnError(t *testing.T) {
	fc := agenttest.NewFakeClient(agenttest.FakeTurn{Err: errWireDead})
	d, _, errOut := testChatDeps(t, "domanda\n", fc)
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop should swallow a turn error and continue: %v", err)
	}
	if !strings.Contains(errOut.String(), "turn error:") {
		t.Fatalf("turn error not reported to errOut:\n%s", errOut.String())
	}
}

type wireDead struct{}

func (wireDead) Error() string { return "wire dead" }

var errWireDead = wireDead{}

// TestRenderTurn_DivergentFlush drives flushRemainder's divergent branch: the
// streamed chunks and the final answer disagree on their prefix, so the renderer
// resets and emits the full final answer (no double-print of the partial stream).
func TestRenderTurn_DivergentFlush(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		// stream a partial chunk that the final answer does NOT extend
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "AAA"}}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "ZZZ final", FinishReason: "stop"},
		}, nil)
	}
	var buf bytes.Buffer
	answer, finish, _, _, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("renderRunnerTurn: %v", err)
	}
	if finish != "stop" {
		t.Errorf("finish = %q, want stop", finish)
	}
	if answer != "ZZZ final" {
		t.Errorf("answer = %q, want the divergent final answer", answer)
	}
}

// TestRenderTurn_ContentStopFallback drives the streamed-chunk path where the final
// answer EXTENDS the streamed prefix (flushRemainder's prefix branch): only the
// not-yet-streamed tail is emitted.
func TestRenderTurn_ContentStopFallback(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "Ciao "}}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "Ciao mondo", FinishReason: "stop"},
		}, nil)
	}
	var buf bytes.Buffer
	answer, _, _, _, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("renderRunnerTurn: %v", err)
	}
	if answer != "Ciao mondo" {
		t.Errorf("answer = %q, want Ciao mondo", answer)
	}
	if n := strings.Count(buf.String(), "Ciao"); n != 1 {
		t.Errorf("'Ciao' rendered %d times, want 1 (tail-only flush, no double-print)", n)
	}
}

// TestRenderTurn_FinalEqualsStreamed drives flushRemainder's exact-match early
// return: the streamed chunks already produced the entire final answer, so the
// final-Event flush emits nothing more (no double-print).
func TestRenderTurn_FinalEqualsStreamed(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{Content: "completo"}}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "completo", FinishReason: "stop"},
		}, nil)
	}
	var buf bytes.Buffer
	answer, _, _, _, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("renderRunnerTurn: %v", err)
	}
	if answer != "completo" {
		t.Errorf("answer = %q, want completo", answer)
	}
	if n := strings.Count(buf.String(), "completo"); n != 1 {
		t.Errorf("'completo' rendered %d times, want exactly 1 (exact-match flush is a no-op)", n)
	}
}

// TestRenderTurn_InfraError surfaces the iter.Seq2 error slot from renderTurn.
func TestRenderTurn_InfraError(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		yield(nil, errWireDead)
	}
	var buf bytes.Buffer
	if _, _, _, _, err := renderRunnerTurn(&buf, seq); err == nil {
		t.Fatal("renderRunnerTurn must surface the error slot")
	}
}

// TestUsageFromStateDelta covers the numeric-widening + nil paths usageFromStateDelta
// + anyInt/anyFloat must tolerate (in-process ints AND JSON-widened int64/float64).
func TestUsageFromStateDelta(t *testing.T) {
	t.Run("nil_map", func(t *testing.T) {
		if u := usageFromStateDelta(nil); u != (llm.Usage{}) {
			t.Errorf("nil map -> %+v, want zero Usage", u)
		}
	})
	t.Run("int_and_cost_float", func(t *testing.T) {
		u := usageFromStateDelta(map[string]any{
			"prompt_tokens":     int(7),
			"completion_tokens": int64(3),
			"cache_hit_tokens":  float64(2),
			"cost_usd":          float64(0.5),
		})
		if u.PromptTokens != 7 || u.CompletionTokens != 3 || u.CachedTokens != 2 {
			t.Errorf("tokens = %+v, want 7/3/2 across int/int64/float64", u)
		}
		if u.Cost == nil || *u.Cost != 0.5 {
			t.Errorf("Cost = %v, want 0.5", u.Cost)
		}
	})
	t.Run("cost_as_int", func(t *testing.T) {
		u := usageFromStateDelta(map[string]any{"cost_usd": int(1)})
		if u.Cost == nil || *u.Cost != 1.0 {
			t.Errorf("int cost_usd -> %v, want 1.0", u.Cost)
		}
	})
	t.Run("unknown_types_default", func(t *testing.T) {
		// A string value hits anyInt's default(0) and anyFloat's default(_,false).
		u := usageFromStateDelta(map[string]any{
			"prompt_tokens": "nope",
			"cost_usd":      "nope",
		})
		if u.PromptTokens != 0 {
			t.Errorf("unknown-type prompt_tokens -> %d, want 0", u.PromptTokens)
		}
		if u.Cost != nil {
			t.Errorf("unknown-type cost_usd -> %v, want nil (no fabricated cost)", u.Cost)
		}
	})
	// M-07: a jsonb round-trip (or a UseNumber decoder) widens token counts to
	// json.Number; anyInt must parse them, not silently zero the usage row. A
	// non-numeric json.Number falls back to 0 like any other unparseable input.
	t.Run("json_number_tokens", func(t *testing.T) {
		u := usageFromStateDelta(map[string]any{
			"prompt_tokens":     json.Number("11"),
			"completion_tokens": json.Number("5"),
			"cache_hit_tokens":  json.Number("nope"),
		})
		if u.PromptTokens != 11 || u.CompletionTokens != 5 {
			t.Errorf("json.Number tokens = %d/%d, want 11/5", u.PromptTokens, u.CompletionTokens)
		}
		if u.CachedTokens != 0 {
			t.Errorf("non-numeric json.Number -> %d, want 0 fallback", u.CachedTokens)
		}
	})
	// M-07 sibling: anyFloat must parse a json.Number cost_usd too, not zero it.
	// A non-numeric json.Number falls back to no-cost like any unparseable input.
	t.Run("json_number_cost", func(t *testing.T) {
		u := usageFromStateDelta(map[string]any{"cost_usd": json.Number("4.2")})
		if u.Cost == nil || *u.Cost != 4.2 {
			t.Errorf("json.Number cost_usd -> %v, want 4.2", u.Cost)
		}
		bad := usageFromStateDelta(map[string]any{"cost_usd": json.Number("nope")})
		if bad.Cost != nil {
			t.Errorf("non-numeric json.Number cost_usd -> %v, want nil fallback", bad.Cost)
		}
	})
}

// TestConfigGet_Success drives configGet's print path (no os.Exit) under a temp
// HOME, covering getConfigKey for every dotted key.
func TestConfigGet_Success(t *testing.T) {
	withTempHome(t)
	for _, key := range []string{
		"llm.provider", "llm.model", "llm.base_url", "llm.temperature",
		"llm.max_tokens", "llm.total_timeout_sec", "llm.connect_timeout_sec",
	} {
		out := captureStdout(t, func() { configGet([]string{key}) })
		if strings.TrimSpace(out) == "" {
			t.Errorf("configGet %q printed nothing", key)
		}
	}
}

// TestGetConfigKey_Unknown covers getConfigKey's miss branch.
func TestGetConfigKey_Unknown(t *testing.T) {
	cfg := &llm.Config{}
	if _, ok := getConfigKey(cfg, "llm.nope"); ok {
		t.Error("getConfigKey returned ok for an unknown key")
	}
}

// TestSetConfigKey_AllKeys covers every setConfigKey branch: the string keys, the
// float key, all three int keys, the unknown-key error, and the bad-float error.
func TestSetConfigKey_AllKeys(t *testing.T) {
	raw := map[string]json.RawMessage{}
	good := [][2]string{
		{"llm.provider", "openrouter"},
		{"llm.model", "x/y"},
		{"llm.base_url", "https://h/v1"},
		{"llm.temperature", "0.2"},
		{"llm.max_tokens", "100"},
		{"llm.total_timeout_sec", "60"},
		{"llm.connect_timeout_sec", "5"},
	}
	for _, kv := range good {
		if err := setConfigKey(raw, kv[0], kv[1]); err != nil {
			t.Errorf("setConfigKey(%q,%q) errored: %v", kv[0], kv[1], err)
		}
	}
	if err := setConfigKey(raw, "llm.temperature", "hot"); err == nil {
		t.Error("bad float temperature should error")
	}
	if err := setConfigKey(raw, "llm.total_timeout_sec", "slow"); err == nil {
		t.Error("bad int total_timeout_sec should error")
	}
	if err := setConfigKey(raw, "llm.unknown", "x"); err == nil {
		t.Error("unknown key should error")
	}
}

// TestSetConfigKey_ConnectTimeoutError covers the connect_timeout_sec setIntKey
// error branch (the total_timeout branch is covered elsewhere; this hits the
// distinct connect_timeout case).
func TestSetConfigKey_ConnectTimeoutError(t *testing.T) {
	raw := map[string]json.RawMessage{}
	if err := setConfigKey(raw, "llm.connect_timeout_sec", "soon"); err == nil {
		t.Error("bad int connect_timeout_sec should error")
	}
}

// TestLoadLLMConfigTolerant_ReloadError covers loadLLMConfigTolerant's re-load
// error branch: with no API key (so the first Load returns ErrMissingAPIKey) AND a
// malformed llm.json, the placeholder-key re-load fails on the parse error, which
// must surface (not be swallowed like the missing-key case).
func TestLoadLLMConfigTolerant_ReloadError(t *testing.T) {
	path := withTempHome(t) // clears OPENROUTER_API_KEY
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLLMConfigTolerant(); err == nil {
		t.Fatal("loadLLMConfigTolerant must surface a malformed-config error on re-load")
	}
}

// TestReadConfigFile_NonNotExistError covers readConfigFile's non-NotExist read
// error branch (the path is a directory, not a file).
func TestReadConfigFile_NonNotExistError(t *testing.T) {
	dir := t.TempDir()
	asDir := filepath.Join(dir, "llm.json")
	if err := os.MkdirAll(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(asDir); err == nil {
		t.Fatal("readConfigFile must error when the path is a directory")
	}
}

// TestWriteConfigFile_MkdirError covers writeConfigFile's MkdirAll error branch:
// the config dir's parent is a regular file, so creating the dir fails.
func TestWriteConfigFile_MkdirError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Path nests UNDER a regular file, so MkdirAll(filepath.Dir(path)) must fail.
	bad := filepath.Join(blocker, "sub", "llm.json")
	if err := writeConfigFile(bad, map[string]json.RawMessage{}); err == nil {
		t.Fatal("writeConfigFile must error when the config dir cannot be created")
	}
}

// TestReadConfigFile_ParseError covers readConfigFile's malformed-JSON branch.
func TestReadConfigFile_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(path); err == nil {
		t.Fatal("readConfigFile must error on malformed JSON")
	}
}

// TestConfigShow_FileTier asserts configShow renders the file-tier values written
// by configSet (drives configShow's full print path + loadLLMConfigTolerant).
func TestConfigShow_FileTier(t *testing.T) {
	withTempHome(t)
	// Write a real file via configSet so show reflects it.
	configSet([]string{"llm.base_url", "https://shown.example/v1"})
	out := captureStdout(t, configShow)
	if !strings.Contains(out, "https://shown.example/v1") {
		t.Fatalf("configShow did not reflect the file-tier base_url:\n%s", out)
	}
}

// TestChat_CtrlCAbort drives chatLoop's first-Ctrl+C abort branch (D-10/D-29): a
// turnCtx that is already cancelled makes runOneTurn return context.Canceled, so
// chatLoop prints the interrupt notice and drops the un-answered user turn.
func TestChat_CtrlCAbort(t *testing.T) {
	fc := agenttest.NewFakeClient(
		textResponseTurn("c1", "non dovrebbe arrivare", llm.Usage{PromptTokens: 1}),
	)
	d, out, _ := testChatDeps(t, "domanda\n", fc)
	// Replace the turn ctx with one cancelled before the run, so aborted() is true.
	d.newTurnCtx = func(parent context.Context) (context.Context, context.CancelFunc, func() bool) {
		ctx, cancel := context.WithCancel(parent)
		cancel() // pre-cancel: the turn is aborted before it streams
		return ctx, func() {}, func() bool { return true }
	}
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	if !strings.Contains(out.String(), "interrotto") {
		t.Fatalf("Ctrl+C abort notice not printed:\n%s", out.String())
	}
}

// TestChat_NonEOFReadError drives chatLoop's non-EOF readErr return: a reader that
// errors with something other than io.EOF surfaces as the chatLoop error.
func TestChat_NonEOFReadError(t *testing.T) {
	fc := agenttest.NewFakeClient()
	d, _, _ := testChatDeps(t, "", fc)
	d.in = &badReader{}
	if err := chatLoop(context.Background(), d); err == nil {
		t.Fatal("chatLoop must surface a non-EOF read error")
	}
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errWireDead }

// TestTrimLine covers trimLine's leading/trailing-space trimming and CR/LF strip.
func TestTrimLine(t *testing.T) {
	cases := map[string]string{
		"  /exit  \n":    "/exit",
		"ciao\r\n":       "ciao",
		"\ufeffciao\r\n": "ciao",
		"   ":            "",
		"a b\n":          "a b", // interior spaces preserved
	}
	for in, want := range cases {
		if got := trimLine(in); got != want {
			t.Errorf("trimLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTurnBudget_CleanEnv covers the per-turn budget happy path: on a clean env
// the builder runOneTurn uses returns a non-nil budget with no error.
func TestTurnBudget_CleanEnv(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "")
	b, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil || b == nil {
		t.Fatalf("NewBudget on clean env: b=%v err=%v", b, err)
	}
}

// TestChat_MalformedBudgetEnv_SurfacesError is the regression for the fixed
// silent-nil defect: a malformed AURA_LOOP_* value must surface as a clear turn
// error (never a nil budget that nil-panics the run-loop's first ConsumeStep).
// config.Load does not validate AURA_LOOP_*, so runOneTurn is the first place the
// bad value is caught — and it must abort the turn BEFORE any model call.
func TestChat_MalformedBudgetEnv_SurfacesError(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "not-a-number")
	fc := agenttest.NewFakeClient(
		textResponseTurn("call-1", "should never reach the model", llm.Usage{PromptTokens: 1, CompletionTokens: 1}),
	)
	d, _, errOut := testChatDeps(t, "fai una cosa\n", fc)
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop must keep running and print the turn error, got fatal: %v", err)
	}
	if !strings.Contains(errOut.String(), "budget config") {
		t.Fatalf("malformed AURA_LOOP_* env did not surface a clear budget-config turn error:\n%s", errOut.String())
	}
	if fc.CallCount() != 0 {
		t.Fatalf("a bad budget must abort the turn before the model call, got %d calls", fc.CallCount())
	}
}

// TestSignalTurnCtx exercises signalTurnCtx: the returned ctx is live, aborted() is
// false before any signal, and cancel() unregisters the handler cleanly (no leak —
// TestMain wires goleak).
func TestSignalTurnCtx(t *testing.T) {
	ctx, cancel, aborted := signalTurnCtx(context.Background())
	if aborted() {
		t.Error("aborted() true before any SIGINT")
	}
	if ctx.Err() != nil {
		t.Error("ctx already errored before any SIGINT")
	}
	cancel() // unregister the signal handler
	if !aborted() {
		t.Error("aborted() false after cancel()")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
