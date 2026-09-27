package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
)

type askResult struct {
	answer elicit.Answer
	err    error
}

func questionOf(t *testing.T, raw map[string]any) elicit.Question {
	t.Helper()
	schema, err := elicit.DecodeSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	q, err := elicit.FromSchema("forms", "ask_name", "what is your name", schema)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func nameQuestion(t *testing.T) elicit.Question {
	return questionOf(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	})
}

// openRun starts a run owned by the local identity, as the route resolves it,
// and subscribes to its stream from the first frame.
func openRun(t *testing.T) (*Server, *httptest.Server, *RunSession, <-chan seqEvent) {
	return openRunWith(t, ServerConfig{})
}

func openRunWith(t *testing.T, cfg ServerConfig) (*Server, *httptest.Server, *RunSession, <-chan seqEvent) {
	t.Helper()
	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")}, &fakeConvStore{}, cfg)
	sess, err := s.runs.Start(runParams{runID: "run-" + uuid.NewString(), threadID: "t-forms", identityID: localIdentityID})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, cancel, _ := sess.subscribeFrom(0)
	t.Cleanup(cancel)
	t.Cleanup(sess.finish)
	// Registered last, so it runs first: releases any Ask a test left waiting.
	t.Cleanup(sess.questions.cancelAll)
	return s, srv, sess, ch
}

// ask puts q to the run's asker on its own goroutine.
func ask(ctx context.Context, sess *RunSession, q elicit.Question) <-chan askResult {
	out := make(chan askResult, 1)
	go func() {
		answer, err := sess.questions.Ask(ctx, q)
		out <- askResult{answer: answer, err: err}
	}()
	return out
}

// nextCustom reads the session's stream up to the next CUSTOM frame named name.
func nextCustom(t *testing.T, ch <-chan seqEvent, name string) any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case sev, ok := <-ch:
			if !ok {
				t.Fatalf("the stream closed before a %s frame", name)
			}
			if ce, isCustom := sev.Ev.(*events.CustomEvent); isCustom && ce.Name == name {
				return ce.Value
			}
		case <-timeout:
			t.Fatalf("no %s frame within 5s", name)
			return nil
		}
	}
}

func answerURL(runID, id string) string {
	return "/agent/runs/" + runID + "/elicitations/" + id
}

func answerPost(t *testing.T, srv *httptest.Server, runID, id, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+answerURL(runID, id), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST answer: %v", err)
	}
	return resp.StatusCode, readFullBody(t, resp)
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	resp := mustGet(t, url)
	readFullBody(t, resp)
	return resp.StatusCode
}

func result(t *testing.T, got <-chan askResult) askResult {
	t.Helper()
	select {
	case r := <-got:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Ask never returned")
		return askResult{}
	}
}

func TestAnAnswerIsDeliveredOnce(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	if q.RunID != sess.RunID || q.Server != "forms" || q.Tool != "ask_name" || len(q.Fields) != 1 {
		t.Fatalf("frame = %+v", q)
	}

	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("first answer = %d %s, want 202", status, body)
	}
	if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionAccept || r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.ID != q.ID || resolved.Action != elicit.ActionAccept {
		t.Fatalf("resolved = %+v", resolved)
	}
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"decline"}`); status != http.StatusConflict {
		t.Fatalf("a late answer = %d, want 409", status)
	}
	if sess.questions.close(q.ID, elicit.ActionCancel, true) {
		t.Fatal("an expiry after the answer resolved the question a second time")
	}
}

// Review Focus 5.
func TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{}}`)
	var refusal struct {
		Errors map[string]string `json:"errors"`
	}
	if status != http.StatusUnprocessableEntity || json.Unmarshal([]byte(body), &refusal) != nil || refusal.Errors["name"] != elicit.ProblemRequired {
		t.Fatalf("answer without the required field = %d %s, want 422 naming it", status, body)
	}
	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("the corrected answer = %d %s, want 202: a 422 must leave the question open", status, body)
	}
	if r := result(t, got); r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
}

// The idempotency layer keeps a mutation's response for its replay, so a 422 must
// name the problem and never the value.
func TestARefusedAnswerLeavesNoValueInTheReplayStore(t *testing.T) {
	s, _, sess, ch := openRun(t)
	registry := &memoryHTTPRegistry{}
	s.SetOperationRegistry(registry)
	ask(context.Background(), sess, questionOf(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"token": map[string]any{"type": "string", "maxLength": 8}},
		"required":   []any{"token"},
	}))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	const secret = "sk-live-0123456789abcdef"
	req := httptest.NewRequest(http.MethodPost, answerURL(sess.RunID, q.ID),
		strings.NewReader(`{"action":"accept","content":{"token":"`+secret+`"}}`))
	req = req.WithContext(identityctx.WithIdentityID(req.Context(), localIdentityID))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "answer-1")
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), elicit.ProblemTooLong) {
		t.Fatalf("an over-long secret = %d %s, want 422 %s", rec.Code, rec.Body, elicit.ProblemTooLong)
	}
	if registry.replay == nil {
		t.Fatal("the 422 never reached the replay store, so this proves nothing")
	}
	for where, text := range map[string]string{"response": rec.Body.String(), "replay store": string(registry.replay.Body)} {
		if strings.Contains(text, secret) {
			t.Fatalf("the %s carries the operator's value: %s", where, text)
		}
	}
}

func TestTheRouteRefusesWhatItCannotDeliver(t *testing.T) {
	s, srv, sess, ch := openRun(t)
	ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	foreign, err := s.runs.Start(runParams{runID: "run-88888888-8888-8888-8888-888888888888", threadID: "t-foreign", identityID: "33333333-3333-3333-3333-333333333333"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.finish)
	for name, tc := range map[string]struct {
		runID, id, body string
		want            int
	}{
		"unknown question": {sess.RunID, "no-such-question", `{"action":"decline"}`, http.StatusNotFound},
		"foreign run":      {foreign.RunID, q.ID, `{"action":"decline"}`, http.StatusNotFound},
		"unknown action":   {sess.RunID, q.ID, `{"action":"sure"}`, http.StatusBadRequest},
		"not json":         {sess.RunID, q.ID, `accept`, http.StatusBadRequest},
	} {
		if status, body := answerPost(t, srv, tc.runID, tc.id, tc.body); status != tc.want {
			t.Errorf("%s: %d %s, want %d", name, status, body, tc.want)
		}
	}
	if status := getStatus(t, srv.URL+"/agent/runs/"+foreign.RunID+"/elicitations"); status != http.StatusNotFound {
		t.Errorf("listing a foreign run's questions = %d, want 404", status)
	}
	sess.finish()
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"decline"}`); status != http.StatusGone {
		t.Fatalf("an answer to an ended run = %d, want 410", status)
	}
}

// A reattach whose replay the ring can no longer serve gets a 410 and no frame, so
// the run lists its open forms itself.
func TestAFormTheRingRotatedPastIsStillListed(t *testing.T) {
	_, srv, sess, ch := openRunWith(t, ServerConfig{RunBufferEvents: 8})
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	for i := range 16 {
		sess.append(context.Background(), events.NewCustomEvent("filler", events.WithValue(i)))
	}
	if status := getStatus(t, srv.URL+"/agent/runs/"+sess.RunID+"/events"); status != http.StatusGone {
		t.Fatalf("a full replay after rotation = %d, want 410, so this proves nothing", status)
	}

	var open struct {
		Questions []elicitationFrame `json:"questions"`
	}
	list := func() {
		t.Helper()
		resp := mustGet(t, srv.URL+"/agent/runs/"+sess.RunID+"/elicitations")
		if body := readFullBody(t, resp); resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &open) != nil {
			t.Fatalf("list = %d %s", resp.StatusCode, body)
		}
	}
	list()
	if len(open.Questions) != 1 || open.Questions[0].ID != q.ID || open.Questions[0].RunID != sess.RunID || len(open.Questions[0].Fields) != 1 {
		t.Fatalf("listed = %+v, want the one open form", open.Questions)
	}
	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("answering the listed form = %d %s, want 202", status, body)
	}
	if r := result(t, got); r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
	if list(); len(open.Questions) != 0 {
		t.Fatalf("listed after the answer = %+v, want none", open.Questions)
	}
}

func TestAnExpiredQuestionIsResolvedAndClosed(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	got := ask(ctx, sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	cancel(elicit.ErrExpired)
	if r := result(t, got); !errors.Is(r.err, elicit.ErrExpired) {
		t.Fatalf("Ask = %+v, want the expiry back", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel || !resolved.Expired {
		t.Fatalf("resolved = %+v, want an expired cancel", resolved)
	}
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusConflict {
		t.Fatalf("an answer after expiry = %d, want 409", status)
	}
}

func TestACallThatEndsCancelsItsQuestion(t *testing.T) {
	_, _, sess, ch := openRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	got := ask(ctx, sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)

	cancel()
	if r := result(t, got); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Ask = %+v, want the call's cancellation", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel || resolved.Expired {
		t.Fatalf("resolved = %+v, want a cancel", resolved)
	}
}

func TestTheRunEndingCancelsEveryPendingQuestion(t *testing.T) {
	_, _, sess, ch := openRun(t)
	first := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)
	second := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)

	sess.questions.cancelAll()
	for _, got := range []<-chan askResult{first, second} {
		if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionCancel {
			t.Fatalf("Ask = %+v, want cancel when the run ends", r)
		}
	}
	for range 2 {
		if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel {
			t.Fatalf("resolved = %+v", resolved)
		}
	}
	if r, err := sess.questions.Ask(context.Background(), nameQuestion(t)); err != nil || r.Action != elicit.ActionCancel {
		t.Fatalf("a question put after the run ended = %+v, %v; want cancel at once", r, err)
	}
}

// Each session is capped in mcptools; a run that reaches several servers is
// capped here.
func TestARunOpensNoMoreThanTheCap(t *testing.T) {
	_, _, sess, ch := openRun(t)
	for range elicit.MaxOpenQuestions {
		ask(context.Background(), sess, nameQuestion(t))
		nextCustom(t, ch, ElicitationEventName)
	}
	if r, err := sess.questions.Ask(context.Background(), nameQuestion(t)); err != nil || r.Action != elicit.ActionDecline {
		t.Fatalf("a question over the run's cap = %+v, %v; want a decline at once", r, err)
	}
	if n := len(sess.questions.open()); n != elicit.MaxOpenQuestions {
		t.Fatalf("%d questions open, want the cap of %d", n, elicit.MaxOpenQuestions)
	}
}

func TestARefusedQuestionIsShownAlreadyResolved(t *testing.T) {
	_, _, sess, ch := openRun(t)
	q := elicit.Question{Server: "forms", Tool: "ask_name", Refusal: elicit.RefusalAmbiguousRun}

	if r, err := sess.questions.Ask(context.Background(), q); err != nil || r.Action != elicit.ActionDecline {
		t.Fatalf("Ask = %+v, %v; a refusal declines at once", r, err)
	}
	if shown := nextCustom(t, ch, ElicitationEventName).(elicitationFrame); shown.Refusal != elicit.RefusalAmbiguousRun {
		t.Fatalf("shown = %+v", shown)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionDecline {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestTheEventsCarryNoAnswerValues(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Zebedee"}}`)
	result(t, got)

	sess.finish()
	replay, _, _ := sess.subscribeFrom(0)
	for sev := range replay {
		raw, err := json.Marshal(sev.Ev)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Zebedee") {
			t.Fatalf("frame %d carries the operator's value: %s", sev.Seq, raw)
		}
	}
}

func TestPublishInterleavesWithTheProducer(t *testing.T) {
	_, _, sess, ch := openRun(t)
	// CUSTOM is a lifecycle frame: append blocks on a subscriber that stops reading.
	go func() {
		for range ch {
		}
	}()
	const each = 200
	var wg sync.WaitGroup
	for _, publisher := range []func(context.Context, events.Event) bool{sess.append, sess.publish} {
		wg.Go(func() {
			for i := range each {
				publisher(context.Background(), events.NewCustomEvent("probe", events.WithValue(i)))
			}
		})
	}
	wg.Wait()
	sess.finish()
	replay, _, _ := sess.subscribeFrom(0)
	var last int64
	count := 0
	for sev := range replay {
		if sev.Seq != last+1 {
			t.Fatalf("seq %d after %d: a publish broke the ring's order", sev.Seq, last)
		}
		last = sev.Seq
		count++
	}
	if count != 2*each {
		t.Fatalf("replayed %d frames, want %d", count, 2*each)
	}
}

// A resolution is bounded by the run, not by the call that asked: a tab that stops
// reading holds the session only until the run ends, and the frame still reaches
// the ring for a replay.
func TestAResolutionGivesUpWhenTheRunEnds(t *testing.T) {
	_, _, sess, ch := openRun(t)
	runCtx, endRun := context.WithCancel(context.Background())
	sess.questions.bind(runCtx)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	// Fill the stalled subscriber's channel, so the next lifecycle frame blocks.
	for i := range fanoutBuffer {
		sess.append(context.Background(), events.NewCustomEvent("filler", events.WithValue(i)))
	}

	delivered := make(chan error, 1)
	go func() {
		_, err := sess.questions.answer(q.ID, elicit.Answer{Action: elicit.ActionDecline})
		delivered <- err
	}()
	select {
	case <-delivered:
		t.Fatal("the resolution did not wait for the stalled tab, so this proves nothing")
	case <-time.After(100 * time.Millisecond):
	}
	endRun()
	select {
	case err := <-delivered:
		if err != nil {
			t.Fatalf("answer = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the resolution still holds the session after the run ended")
	}
	if r := result(t, got); r.answer.Action != elicit.ActionDecline {
		t.Fatalf("Ask = %+v", r)
	}
}

func TestOpenQuestionsStayInArrivalOrder(t *testing.T) {
	_, _, sess, ch := openRun(t)
	var ids []string
	for i := range elicit.MaxOpenQuestions {
		q := nameQuestion(t)
		q.Deadline = time.Now().Add(time.Duration(elicit.MaxOpenQuestions-i) * time.Minute)
		ask(context.Background(), sess, q)
		ids = append(ids, nextCustom(t, ch, ElicitationEventName).(elicitationFrame).ID)
	}
	for i, q := range sess.questions.open() {
		if q.ID != ids[i] {
			t.Fatalf("question %d = %s, want arrival %s", i, q.ID, ids[i])
		}
	}
}

func TestFinishingASessionLeavesNoOpenForms(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)
	sess.finish()
	if open := sess.questions.open(); len(open) != 0 {
		t.Fatalf("terminal session still lists %d open forms", len(open))
	}
	if r := result(t, got); r.answer.Action != elicit.ActionCancel {
		t.Fatalf("Ask = %+v, want cancel", r)
	}
	if body := readFullBody(t, mustGet(t, srv.URL+"/agent/runs/"+sess.RunID+"/elicitations")); !strings.Contains(body, `"questions":[]`) {
		t.Fatalf("terminal list = %s", body)
	}
}

func TestAnAnswerReplayNeverDeliversTwice(t *testing.T) {
	s, _, sess, ch := openRun(t)
	registry := &memoryHTTPRegistry{}
	s.SetOperationRegistry(registry)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	mux := s.Mux()
	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, answerURL(sess.RunID, q.ID),
			strings.NewReader(`{"action":"accept","content":{"name":"private-answer"}}`))
		req = req.WithContext(identityctx.WithIdentityID(req.Context(), localIdentityID))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "accepted-answer")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted || (i == 1 && rec.Header().Get("Idempotency-Replayed") != "true") {
			t.Fatalf("answer %d = %d %s, want delivered/replayed 202", i, rec.Code, rec.Body)
		}
	}
	if r := result(t, got); r.answer.Content["name"] != "private-answer" {
		t.Fatalf("Ask = %+v", r)
	}
	if registry.replay == nil || strings.Contains(string(registry.replay.Body), "private-answer") {
		t.Fatalf("replay = %+v, want a response without answer values", registry.replay)
	}
}
