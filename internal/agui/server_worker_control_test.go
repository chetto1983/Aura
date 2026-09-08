package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
)

type controlSteerProbe struct {
	pushes   []string
	err      error
	op       bool
	receipts []steer.WorkerReceipt
}

func (f *controlSteerProbe) Push(string, string, string) error {
	return errors.New("child reached parent inbox")
}
func (f *controlSteerProbe) PushWorker(ctx context.Context, conv, child, run, text string) error {
	if f.err != nil {
		return f.err
	}
	_, f.op = idempotency.OperationFromContext(ctx)
	f.pushes = append(f.pushes, conv+"/"+child+"/"+run+":"+text)
	return nil
}
func (f *controlSteerProbe) WorkerHistory(context.Context, string, string) ([]steer.WorkerReceipt, error) {
	return append([]steer.WorkerReceipt{}, f.receipts...), f.err
}

func workerControlServer(t *testing.T) (*Server, agent.WorkerControlSession, *controlSteerProbe, *int) {
	t.Helper()
	s := NewServer(&scriptedRunner{}, newOwnerConvStore(controlConversation, controlOwner), ServerConfig{})
	s.runs = NewRunRegistry(ServerConfig{})
	t.Cleanup(s.runs.Close)
	stops := new(int)
	p := registryWorkerParams("w1")
	p.Stop = func(context.Context) error { *stops++; return nil }
	control, err := s.runs.StartWorker(controlContext(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Finish() })
	probe := &controlSteerProbe{}
	s.SetSteerInbox(probe)
	return s, control, probe, stops
}

func callWorkerRoute(s *Server, owner, method, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(identityctx.WithIdentityID(r.Context(), owner))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Mux().ServeHTTP(w, r)
	return w
}

func workerBody(run, text string) string {
	b, _ := json.Marshal(workerControlRequest{RunID: run, Text: text})
	return string(b)
}

const workerRoutePrefix = "/api/conversations/" + controlConversation + "/swarm/w1/"

func TestWorkerSteerUsesNativeOperationReplayAndTargetScope(t *testing.T) {
	s, c, probe, _ := workerControlServer(t)
	s.SetOperationRegistry(&memoryHTTPRegistry{})
	for range 2 {
		w := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+"steer", workerBody(c.RunID, "use JSON"), "same-logical-send")
		if w.Code != http.StatusAccepted {
			t.Fatalf("steer: %d %s", w.Code, w.Body.String())
		}
	}
	if len(probe.pushes) != 1 || !probe.op || !strings.Contains(probe.pushes[0], "/w1/"+c.RunID+":use JSON") {
		t.Fatalf("wrong dispatch/replay: %+v", probe)
	}
	changed := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+"steer", workerBody(c.RunID, "different request"), "same-logical-send")
	if changed.Code != http.StatusConflict || len(probe.pushes) != 1 {
		t.Fatal("same key accepted different intent")
	}
}

func TestWorkerControlsRejectForeignWrongAndTerminalTargets(t *testing.T) {
	s, c, probe, stops := workerControlServer(t)
	for _, tc := range []struct{ owner, path, body string }{
		{"33333333-3333-3333-3333-333333333333", workerRoutePrefix + "steer", workerBody(c.RunID, "foreign")},
		{controlOwner, strings.Replace(workerRoutePrefix, "w1/", "w2/", 1) + "steer", workerBody(c.RunID, "sibling")},
		{controlOwner, workerRoutePrefix + "cancel", workerBody("run-unknown", "")},
		{controlOwner, "/agent/runs/" + c.RunID + "/cancel", "{}"},
	} {
		w := callWorkerRoute(s, tc.owner, "POST", tc.path, tc.body, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("wrong target: %d %s", w.Code, w.Body.String())
		}
	}
	if len(probe.pushes) != 0 || *stops != 0 {
		t.Fatal("refused target caused a side effect")
	}
	w := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+"cancel", workerBody(c.RunID, ""), "")
	if w.Code != http.StatusAccepted || *stops != 1 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	if err := c.Finish(); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"steer", "cancel"} {
		w := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+action, workerBody(c.RunID, ""), "")
		if w.Code != http.StatusGone {
			t.Fatalf("terminal %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	if *stops != 1 {
		t.Fatal("terminal worker stopped twice")
	}
}

func TestWorkerSteerRefusalsAndRequiredIdempotency(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{steer.ErrEmpty, 400}, {steer.ErrTooLarge, 400}, {steer.ErrQueueFull, 429}} {
		s, c, probe, _ := workerControlServer(t)
		probe.err = tc.err
		w := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+"steer", workerBody(c.RunID, "message"), "")
		if w.Code != tc.status {
			t.Fatalf("refusal: %d", w.Code)
		}
	}
	s, c, _, _ := workerControlServer(t)
	s.SetOperationRegistry(&memoryHTTPRegistry{})
	for _, action := range []string{"steer", "cancel"} {
		w := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+action, workerBody(c.RunID, ""), "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing operation key for %s: %d", action, w.Code)
		}
	}
}

func TestWorkerHistoryDistinguishesAcceptedAppliedAndLostOwner(t *testing.T) {
	s, c, probe, _ := workerControlServer(t)
	probe.receipts = []steer.WorkerReceipt{{ID: "a", RunID: c.RunID, Status: "accepted"}, {ID: "b", RunID: "old-run", Status: "applied"}, {ID: "c", RunID: "lost-run", Status: "accepted"}}
	w := callWorkerRoute(s, controlOwner, "GET", workerRoutePrefix+"controls", "", "")
	if w.Code != 200 {
		t.Fatalf("history: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private control history is cacheable")
	}
	var body struct {
		Receipts []steer.WorkerReceipt `json:"receipts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Receipts) != 3 || body.Receipts[0].Status != "accepted" || body.Receipts[1].Status != "applied" || body.Receipts[2].Reason != "owner_unavailable" {
		t.Fatalf("history: %+v", body)
	}
	foreign := callWorkerRoute(s, "33333333-3333-3333-3333-333333333333", "GET", workerRoutePrefix+"controls", "", "")
	if foreign.Code != 404 || strings.Contains(foreign.Body.String(), "lost-run") {
		t.Fatal("foreign receipt history leaked")
	}
}
