package agui

import (
	"context"
	"io"
	"iter"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

// fakeCapSource is a scripted llm.ReasoningCapabilitySource: it returns a fixed advertised set
// + detected flag so the two-stage governance tests drive every branch (advertised / not /
// mandatory-no-none / detection-failed) with NO network or live model.
type fakeCapSource struct {
	efforts  []llm.ReasoningEffort
	deflt    llm.ReasoningEffort
	detected bool
}

func (f fakeCapSource) AllowedEfforts(context.Context) ([]llm.ReasoningEffort, llm.ReasoningEffort, bool) {
	return f.efforts, f.deflt, f.detected
}

// effortRunner wraps scriptedRunner to CAPTURE the ctx handleRun drives the turn with, so a
// test can assert (via the exported runner.ReasoningOverride reader) that a validated fixed
// level was actually threaded onto ctx — and that auto/absent threads nothing.
type effortRunner struct {
	*scriptedRunner
	gotCtx context.Context
}

func (r *effortRunner) Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error] {
	r.gotCtx = ctx
	return r.scriptedRunner.Turn(ctx, convID, userMsg)
}

func (r *effortRunner) TurnWithModelUserMessage(ctx context.Context, convID string, visible, model string) iter.Seq2[*agent.Event, error] {
	r.gotCtx = ctx
	return r.scriptedRunner.TurnWithModelUserMessage(ctx, convID, visible, model)
}

// effortConvStore wraps fakeConvStore to record the symbol persisted through
// UpdateReasoningEffortForIdentity so a test can assert the exact persisted value (incl. "" for
// auto/absent). updateErr injects a persist failure to prove it is logged-not-fatal.
type effortConvStore struct {
	*fakeConvStore
	gotEffort   string
	gotThreadID string
	effortCalls int
	updateErr   error
}

func (c *effortConvStore) UpdateReasoningEffortForIdentity(_ context.Context, convID, _, effort string) (int64, error) {
	c.effortCalls++
	c.gotThreadID = convID
	c.gotEffort = effort
	if c.updateErr != nil {
		return 0, c.updateErr
	}
	return 1, nil
}

func effortRunBody(tid, effort string) string {
	return `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"ciao"}],"aura":{"effort":"` + effort + `"}}`
}

// newEffortServer builds a live httptest server with the given runner, conv store, and (optional)
// capability source wired — the SetReasoningCapabilitySource injection the composition root does.
func newEffortServer(t *testing.T, run Runner, conv ConversationStore, src llm.ReasoningCapabilitySource) *httptest.Server {
	t.Helper()
	s := NewServer(run, conv, ServerConfig{})
	s.idgen = &fixedIDGen{}
	s.SetReasoningCapabilitySource(src, "openrouter")
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

// TestParseEffortSymbol pins the pure 7-symbol enum → (effort, isFixed, ok) map, incl. the
// UI→internal translation (mid→medium, extra→xhigh, max→max) and the strict rejection of
// non-enum values — the internal names (medium/xhigh) and any casing/garbage are ok=false.
func TestParseEffortSymbol(t *testing.T) {
	tests := []struct {
		symbol    string
		wantEff   llm.ReasoningEffort
		wantFixed bool
		wantOK    bool
	}{
		{"", "", false, true},
		{"auto", "", false, true},
		{"off", llm.ReasoningEffortNone, true, true},
		{"low", llm.ReasoningEffortLow, true, true},
		{"mid", llm.ReasoningEffortMedium, true, true},
		{"high", llm.ReasoningEffortHigh, true, true},
		{"extra", llm.ReasoningEffortXHigh, true, true},
		{"max", llm.ReasoningEffortMax, true, true},
		// Non-enum / injection surface → ok=false → 400 upstream.
		{"turbo", "", false, false},
		{"medium", "", false, false}, // the INTERNAL name is not a UI symbol
		{"xhigh", "", false, false},
		{"minimal", "", false, false},
		{"Auto", "", false, false}, // case-sensitive: the UI sends lowercase symbols only
		{"OFF", "", false, false},
		{"  high  ", "", false, false}, // no trimming — an exact symbol only
	}
	for _, tc := range tests {
		gotEff, gotFixed, gotOK := parseEffortSymbol(tc.symbol)
		if gotEff != tc.wantEff || gotFixed != tc.wantFixed || gotOK != tc.wantOK {
			t.Errorf("parseEffortSymbol(%q) = (%q,%v,%v), want (%q,%v,%v)",
				tc.symbol, gotEff, gotFixed, gotOK, tc.wantEff, tc.wantFixed, tc.wantOK)
		}
	}
}

// TestHandleRunEffort covers Stage-1 syntactic governance + the isolation-precedes-governance
// ordering: a non-enum symbol → 400; absent/auto → 200 with NO override threaded but the symbol
// still persisted; a foreign thread → 404 BEFORE effort validation.
func TestHandleRunEffort(t *testing.T) {
	const tid = "11111111-1111-1111-1111-111111111111"
	permissive := fakeCapSource{efforts: []llm.ReasoningEffort{
		llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium,
		llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax,
	}, detected: true}

	t.Run("non-enum symbol → 400", func(t *testing.T) {
		run := &effortRunner{scriptedRunner: &scriptedRunner{events: textTurn("ok")}}
		conv := &effortConvStore{fakeConvStore: &fakeConvStore{known: map[string]bool{tid: true}}}
		srv := newEffortServer(t, run, conv, permissive)
		resp := postRun(t, srv, effortRunBody(tid, "turbo"))
		defer resp.Body.Close()
		body := readBody(t, resp.Body)
		if resp.StatusCode != 400 {
			t.Fatalf("status = %d, want 400 for a non-enum effort", resp.StatusCode)
		}
		if !strings.Contains(body, "invalid reasoning effort") {
			t.Errorf("400 body = %q, want the Stage-1 message", body)
		}
		if run.turnCalled {
			t.Error("Turn ran on a rejected effort — the 400 must precede the turn")
		}
		if conv.effortCalls != 0 {
			t.Error("a rejected effort must NOT be persisted")
		}
	})

	t.Run("absent effort → 200, no override, persists empty", func(t *testing.T) {
		run := &effortRunner{scriptedRunner: &scriptedRunner{events: textTurn("ok")}}
		conv := &effortConvStore{fakeConvStore: &fakeConvStore{known: map[string]bool{tid: true}}}
		srv := newEffortServer(t, run, conv, permissive)
		resp := postRun(t, srv, runPayload(tid)) // no aura envelope at all
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if _, ok := runner.ReasoningOverride(run.gotCtx); ok {
			t.Error("absent effort threaded an override; want none (adaptive path, D-04)")
		}
		if conv.effortCalls != 1 || conv.gotEffort != "" {
			t.Errorf("persist = (calls %d, effort %q), want (1, \"\")", conv.effortCalls, conv.gotEffort)
		}
	})

	t.Run("auto → 200, no override, persists auto", func(t *testing.T) {
		run := &effortRunner{scriptedRunner: &scriptedRunner{events: textTurn("ok")}}
		conv := &effortConvStore{fakeConvStore: &fakeConvStore{known: map[string]bool{tid: true}}}
		srv := newEffortServer(t, run, conv, permissive)
		resp := postRun(t, srv, effortRunBody(tid, "auto"))
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if _, ok := runner.ReasoningOverride(run.gotCtx); ok {
			t.Error("auto threaded an override; want none")
		}
		if conv.gotEffort != "auto" {
			t.Errorf("persisted %q, want \"auto\" (switch-back remembered, OQ-3)", conv.gotEffort)
		}
	})

	t.Run("foreign thread 404 precedes effort validation", func(t *testing.T) {
		const foreign = "22222222-2222-2222-2222-222222222222"
		run := &effortRunner{scriptedRunner: &scriptedRunner{events: textTurn("ok")}}
		conv := &effortConvStore{fakeConvStore: &fakeConvStore{known: map[string]bool{}}} // owns nothing
		srv := newEffortServer(t, run, conv, permissive)
		resp := postRun(t, srv, effortRunBody(foreign, "turbo")) // invalid effort on a foreign thread
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("status = %d, want 404 (isolation precedes governance — not the 400 for the bad effort)", resp.StatusCode)
		}
		if conv.effortCalls != 0 {
			t.Error("a foreign thread must never reach the persist")
		}
	})
}

// TestHandleRunEffortCapability covers Stage-2 governance against a fake capability source:
// advertised fixed → 200 + threaded + persisted; unadvertised → 400; mandatory (no none) + off
// → 400; detection-failed collapses to the safe floor (graduated → 400, off/auto → pass).
func TestHandleRunEffortCapability(t *testing.T) {
	const tid = "33333333-3333-3333-3333-333333333333"

	newSrv := func(t *testing.T, src llm.ReasoningCapabilitySource) (*httptest.Server, *effortRunner, *effortConvStore) {
		run := &effortRunner{scriptedRunner: &scriptedRunner{events: textTurn("ok")}}
		conv := &effortConvStore{fakeConvStore: &fakeConvStore{known: map[string]bool{tid: true}}}
		return newEffortServer(t, run, conv, src), run, conv
	}

	t.Run("advertised fixed → 200 + threaded + persisted", func(t *testing.T) {
		src := fakeCapSource{efforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}, detected: true}
		srv, run, conv := newSrv(t, src)
		resp := postRun(t, srv, effortRunBody(tid, "high"))
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200 for an advertised level", resp.StatusCode)
		}
		eff, ok := runner.ReasoningOverride(run.gotCtx)
		if !ok || eff != llm.ReasoningEffortHigh {
			t.Errorf("threaded override = (%q,%v), want (high,true)", eff, ok)
		}
		if conv.gotEffort != "high" {
			t.Errorf("persisted %q, want \"high\"", conv.gotEffort)
		}
	})

	t.Run("unadvertised fixed → 400 (Stage-2 distinct message)", func(t *testing.T) {
		src := fakeCapSource{efforts: []llm.ReasoningEffort{llm.ReasoningEffortLow}, detected: true}
		srv, run, conv := newSrv(t, src)
		resp := postRun(t, srv, effortRunBody(tid, "high"))
		defer resp.Body.Close()
		body := readBody(t, resp.Body)
		if resp.StatusCode != 400 {
			t.Fatalf("status = %d, want 400 for an unadvertised level", resp.StatusCode)
		}
		if !strings.Contains(body, "not supported by the active model") {
			t.Errorf("400 body = %q, want the Stage-2 (capability) message, distinct from Stage-1", body)
		}
		if run.turnCalled || conv.effortCalls != 0 {
			t.Error("an unadvertised level must neither run the turn nor persist")
		}
	})

	t.Run("mandatory model (no none) + off → 400", func(t *testing.T) {
		// A mandatory model's source omits none/off from the advertised set (excludeNone upstream).
		src := fakeCapSource{efforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}, detected: true}
		srv, _, _ := newSrv(t, src)
		resp := postRun(t, srv, effortRunBody(tid, "off"))
		defer resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("status = %d, want 400 (off is not advertised by a mandatory model)", resp.StatusCode)
		}
	})

	t.Run("detection failed + graduated → 400 (safe floor)", func(t *testing.T) {
		src := fakeCapSource{detected: false}
		srv, _, _ := newSrv(t, src)
		resp := postRun(t, srv, effortRunBody(tid, "high"))
		defer resp.Body.Close()
		body := readBody(t, resp.Body)
		if resp.StatusCode != 400 {
			t.Fatalf("status = %d, want 400 (graduated level not verifiable)", resp.StatusCode)
		}
		if !strings.Contains(body, "not verifiable") {
			t.Errorf("400 body = %q, want the detection-failed safe-floor message", body)
		}
	})

	t.Run("detection failed + off → 200 (off is the universal floor)", func(t *testing.T) {
		src := fakeCapSource{detected: false}
		srv, run, conv := newSrv(t, src)
		resp := postRun(t, srv, effortRunBody(tid, "off"))
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200 (off always passes)", resp.StatusCode)
		}
		if eff, ok := runner.ReasoningOverride(run.gotCtx); !ok || eff != llm.ReasoningEffortNone {
			t.Errorf("threaded override = (%q,%v), want (none,true)", eff, ok)
		}
		if conv.gotEffort != "off" {
			t.Errorf("persisted %q, want \"off\"", conv.gotEffort)
		}
	})

	t.Run("detection failed + auto → 200 (Stage-2 skipped for auto)", func(t *testing.T) {
		src := fakeCapSource{detected: false}
		srv, run, _ := newSrv(t, src)
		resp := postRun(t, srv, effortRunBody(tid, "auto"))
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("status = %d, want 200 (auto always passes)", resp.StatusCode)
		}
		if _, ok := runner.ReasoningOverride(run.gotCtx); ok {
			t.Error("auto threaded an override; want none")
		}
	})
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}
