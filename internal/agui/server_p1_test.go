package agui

import (
	"io"
	"net/http"
	"strings"
	"testing"
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
