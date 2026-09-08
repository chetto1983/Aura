package agui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

type queuedControlProbe struct {
	row       documents.DelegationJobRow
	err       error
	cancelErr error
	calls     []documents.QueuedWorkerCancellationRequest
}

func (p *queuedControlProbe) FindDelegationJob(_ context.Context, owner, conv, child string) (documents.DelegationJobRow, bool, error) {
	return p.row, owner == controlOwner && conv == controlConversation && child == "w1", p.err
}
func (p *queuedControlProbe) RequestQueuedWorkerCancellation(_ context.Context, req documents.QueuedWorkerCancellationRequest) error {
	p.calls = append(p.calls, req)
	if p.cancelErr == nil {
		p.row.OperatorCancelled = true
	}
	return p.cancelErr
}

const queuedControlJobID = "44444444-4444-4444-4444-444444444444"

func TestQueuedWorkerControlsHaveDurableTargetsAndReplay(t *testing.T) {
	s, c, _, stops := workerControlServer(t)
	_ = c.Finish()
	s.SetOperationRegistry(&memoryHTTPRegistry{})
	probe := &queuedControlProbe{row: documents.DelegationJobRow{ID: queuedControlJobID, ChildID: "w1", Status: "queued", MaxAttempts: 3}}
	s.SetWorkerControlJobs(probe)
	w := callWorkerRoute(s, controlOwner, "GET", workerRoutePrefix+"controls", "", "")
	var state struct {
		QueuedTarget    *queuedWorkerTarget `json:"queued_target"`
		CancelRequested bool                `json:"cancel_requested"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || w.Code != 200 || state.QueuedTarget == nil || state.QueuedTarget.JobID != queuedControlJobID {
		t.Fatalf("queued control missing without transcript: %d %s", w.Code, w.Body.String())
	}
	body := `{"job_id":"` + queuedControlJobID + `","attempt_count":0}`
	for range 2 {
		w = callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+"cancel", body, "queued-stop")
		if w.Code != 202 {
			t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
		}
	}
	if len(probe.calls) != 1 || *stops != 0 || probe.calls[0].IdentityID != controlOwner || probe.calls[0].ConversationID != controlConversation || probe.calls[0].ChildID != "w1" {
		t.Fatalf("wrong dispatch: %+v", probe.calls)
	}
	w = callWorkerRoute(s, controlOwner, "GET", workerRoutePrefix+"controls", "", "")
	state.QueuedTarget = nil
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || !state.CancelRequested || state.QueuedTarget != nil {
		t.Fatalf("accepted cancellation lost on reload: %s", w.Body.String())
	}
}

func TestQueuedWorkerControlsRefuseForeignMalformedAndClaimedTargets(t *testing.T) {
	s, _, _, _ := workerControlServer(t)
	probe := &queuedControlProbe{row: documents.DelegationJobRow{ID: queuedControlJobID}}
	s.SetWorkerControlJobs(probe)
	body := `{"job_id":"` + queuedControlJobID + `","attempt_count":0}`
	for _, tc := range []struct {
		owner, body string
		code        int
	}{
		{"33333333-3333-3333-3333-333333333333", body, 404},
		{controlOwner, `{"job_id":"wrong","attempt_count":0}`, 400},
		{controlOwner, `{"job_id":"` + queuedControlJobID + `"}`, 400},
		{controlOwner, `{"job_id":"` + queuedControlJobID + `","attempt_count":-1}`, 400},
		{controlOwner, `{"job_id":"` + queuedControlJobID + `","attempt_count":0,"run_id":"active"}`, 400},
		{controlOwner, `{"job_id":"55555555-5555-5555-5555-555555555555","attempt_count":0}`, 404},
	} {
		w := callWorkerRoute(s, tc.owner, "POST", workerRoutePrefix+"cancel", tc.body, "")
		if w.Code != tc.code {
			t.Fatalf("want %d got %d: %s", tc.code, w.Code, w.Body.String())
		}
	}
	if len(probe.calls) != 0 {
		t.Fatal("invalid request reached store")
	}
	probe.cancelErr = documents.ErrIngestionJobLeaseLost
	w := callWorkerRoute(s, controlOwner, "POST", workerRoutePrefix+"cancel", body, "")
	if w.Code != 410 {
		t.Fatalf("claim race: %d %s", w.Code, w.Body.String())
	}
	probe.err = errors.New("database unavailable")
	for _, action := range []string{"cancel", "controls"} {
		method := "POST"
		if action == "controls" {
			method = "GET"
		}
		if w := callWorkerRoute(s, controlOwner, method, workerRoutePrefix+action, body, ""); w.Code != 503 {
			t.Fatalf("database error: %d", w.Code)
		}
	}
}
