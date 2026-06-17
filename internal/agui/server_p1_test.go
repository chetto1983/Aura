package agui

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
)

type guardedScriptedRunner struct {
	scriptedRunner
	busy bool
}

func (g *guardedScriptedRunner) TryLockThread(_ string) (func(), bool) {
	if g.busy {
		return nil, false
	}
	g.busy = true
	return func() { g.busy = false }, true
}

func TestServerMetricsExposesPrometheus(t *testing.T) {
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
	if !strings.Contains(body, "aura_agent_tool_dispatch_total") {
		t.Fatalf("/metrics missing agent prometheus counters: %s", body)
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

// TestBranchSelect_BusyThread409 proves a branch-select re-run on an already-running
// thread is rejected 409 (it acquires the same per-thread lock as a normal turn — D-09).
func TestBranchSelect_BusyThread409(t *testing.T) {
	const tid = "13131313-1313-1313-1313-131313131313"
	srv := newTestServer(t,
		&guardedScriptedRunner{busy: true},
		// Seq 3 must be a known branch leaf (WR-01 membership gate) so the request
		// reaches the lock check rather than 404ing on the leaf.
		&fakeConvStore{known: map[string]bool{tid: true}, branches: []conversations.Branch{{LeafSeq: 3}}})

	resp, err := http.Post(srv.URL+"/api/conversations/"+tid+"/branches/3/select", "", nil)
	if err != nil {
		t.Fatalf("POST select: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("branch-select on a busy thread status = %d, want 409: %s", resp.StatusCode, raw)
	}
}

// TestBranchSelect_LockAcquiredAndReRuns proves the select re-run acquires the lock,
// drives TurnBranch over the selected leaf, and streams the turn (the happy lock path).
func TestBranchSelect_LockAcquiredAndReRuns(t *testing.T) {
	const tid = "14141414-1414-1414-1414-141414141414"
	run := &guardedScriptedRunner{}
	run.events = textTurn("branch reply")
	// Seq 5 must be a known branch leaf (WR-01 membership gate) for the happy lock path.
	srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}, branches: []conversations.Branch{{LeafSeq: 5}}})

	resp, err := http.Post(srv.URL+"/api/conversations/"+tid+"/branches/5/select", "", nil)
	if err != nil {
		t.Fatalf("POST select: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("branch-select status = %d, want 200: %s", resp.StatusCode, raw)
	}
	if run.gotBranchLeaf != 5 {
		t.Errorf("TurnBranch leaf = %d, want 5 (the selected leaf)", run.gotBranchLeaf)
	}
	if !strings.Contains(string(raw), "branch reply") {
		t.Errorf("branch-select SSE body missing the re-run reply: %s", raw)
	}
}

// TestBranchSelect_UnknownThread404 proves the re-run resolves the thread first: an
// unknown conversation → 404 (thread not found) before any lock/run.
func TestBranchSelect_UnknownThread404(t *testing.T) {
	const tid = "15151515-1515-1515-1515-151515151515"
	srv := newTestServer(t, &guardedScriptedRunner{}, &fakeConvStore{known: map[string]bool{}})
	resp, err := http.Post(srv.URL+"/api/conversations/"+tid+"/branches/3/select", "", nil)
	if err != nil {
		t.Fatalf("POST select: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("branch-select unknown thread status = %d, want 404: %s", resp.StatusCode, raw)
	}
}
