package agui

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type guardedScriptedRunner struct {
	scriptedRunner
	busy bool
}

func (g *guardedScriptedRunner) TryLockThread(_ context.Context, _ string) (func(), bool) {
	if g.busy {
		return nil, false
	}
	g.busy = true
	return func() { g.busy = false }, true
}

func TestServerMetricsCompatibilityExcludesOTelAgentCatalog(t *testing.T) {
	srv := newTestServerCfg(t, nil, nil, ServerConfig{})
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
	}
	body := string(raw)
	if !strings.Contains(body, "aura_agui_sse_dropped_total") {
		t.Fatalf("/metrics missing legacy compatibility counters: %s", body)
	}
	if strings.Contains(body, "aura_agent_tool_dispatch_total") {
		t.Fatalf("public compatibility route exposed OTel-owned agent metric: %s", body)
	}
}

func TestServer_RunBusyThread409(t *testing.T) {
	const tid = "12121212-1212-1212-1212-121212121212"
	srv := newTestServer(t,
		&guardedScriptedRunner{busy: true},
		&fakeConvStore{known: map[string]bool{tid: true}})

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 409: %s", resp.StatusCode, raw)
	}
}

// TestBranchEdit_BusyThread409DoesNotFork proves an edit on an already-running thread is
// rejected 409 by the same per-thread lock as a normal turn, and that the fork waits for
// the lock: a refused edit leaves no orphan turn behind (spec 2026-10-27, D10).
func TestBranchEdit_BusyThread409DoesNotFork(t *testing.T) {
	const tid = "13131313-1313-1313-1313-131313131313"
	store := &fakeConvStore{known: map[string]bool{tid: true}}
	srv := newTestServer(t, &guardedScriptedRunner{busy: true}, store)

	resp, err := http.Post(srv.URL+"/api/conversations/"+tid+"/edit", "application/json",
		strings.NewReader(`{"diverge_seq":3,"content":"edited"}`))
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("edit on a busy thread status = %d, want 409: %s", resp.StatusCode, raw)
	}
	if n := store.forks.Load(); n != 0 {
		t.Fatalf("a busy thread was forked %d time(s), want none", n)
	}
}

// TestBranchEdit_LockAcquiredForksAndReRuns proves the edit takes the lock, forks, drives
// TurnBranch over the fork and streams the turn (the happy lock path).
func TestBranchEdit_LockAcquiredForksAndReRuns(t *testing.T) {
	const tid = "14141414-1414-1414-1414-141414141414"
	run := &guardedScriptedRunner{}
	run.events = textTurn("branch reply")
	store := &fakeConvStore{known: map[string]bool{tid: true}, forkLeaf: 5}
	srv := newTestServer(t, run, store)

	resp, err := http.Post(srv.URL+"/api/conversations/"+tid+"/edit", "application/json",
		strings.NewReader(`{"diverge_seq":3,"content":"edited"}`))
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit status = %d, want 200: %s", resp.StatusCode, raw)
	}
	if store.forks.Load() != 1 || run.gotBranchLeaf != 5 {
		t.Errorf("forks = %d, TurnBranch leaf = %d; want one fork re-run at 5", store.forks.Load(), run.gotBranchLeaf)
	}
	if !strings.Contains(string(raw), "branch reply") {
		t.Errorf("edit SSE body missing the re-run reply: %s", raw)
	}
}

// TestBranchEdit_UnknownThread404 proves the edit resolves the thread first: an unknown
// conversation → 404 before any lock, fork or run.
func TestBranchEdit_UnknownThread404(t *testing.T) {
	const tid = "15151515-1515-1515-1515-151515151515"
	store := &fakeConvStore{known: map[string]bool{}}
	srv := newTestServer(t, &guardedScriptedRunner{}, store)
	resp, err := http.Post(srv.URL+"/api/conversations/"+tid+"/edit", "application/json",
		strings.NewReader(`{"diverge_seq":3,"content":"edited"}`))
	if err != nil {
		t.Fatalf("POST edit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || store.forks.Load() != 0 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("edit unknown thread status = %d (forks %d), want 404 and none: %s", resp.StatusCode, store.forks.Load(), raw)
	}
}
