package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
)

func stalledElicitationRun(t *testing.T) (string, *RunSession, <-chan seqEvent, func()) {
	t.Helper()
	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")}, &fakeConvStore{}, ServerConfig{BufferCap: 1})
	sess, err := s.runs.Start(runParams{runID: "run-" + uuid.NewString(), threadID: "t-stalled-form", identityID: localIdentityID})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, unsubscribe, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("fresh session cannot subscribe")
	}
	return srv.URL, sess, ch, unsubscribe
}

func waitForStalledElicitationPublish(t *testing.T, sess *RunSession) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for sess.mu.TryLock() {
		sess.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("question publisher never entered the stalled subscriber send")
		}
		time.Sleep(time.Millisecond)
	}
}

func assertQuestionThenResolutionInReplay(t *testing.T, sess *RunSession, wantID, wantAction string) {
	t.Helper()
	sess.finish()
	replay, cancelReplay, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("question and resolution fell out of replay")
	}
	defer cancelReplay()
	var questionSeq, resolutionSeq int64
	for sev := range replay {
		custom, ok := sev.Ev.(*events.CustomEvent)
		if !ok {
			continue
		}
		switch custom.Name {
		case ElicitationEventName:
			if frame := custom.Value.(elicitationFrame); frame.ID == wantID {
				questionSeq = sev.Seq
			}
		case ElicitationResolvedEventName:
			if frame := custom.Value.(elicitationResolvedFrame); frame.ID == wantID {
				if frame.Action != wantAction {
					t.Fatalf("resolution action = %q, want %q", frame.Action, wantAction)
				}
				resolutionSeq = sev.Seq
			}
		}
	}
	if questionSeq == 0 || resolutionSeq <= questionSeq {
		t.Fatalf("replay question seq %d, resolution seq %d; want both in order", questionSeq, resolutionSeq)
	}
}

func TestStalledQuestionPublishCannotHoldOpenForms(t *testing.T) {
	baseURL, sess, ch, unsubscribe := stalledElicitationRun(t)
	callCtx, endCall := context.WithCancel(context.Background())
	defer func() {
		endCall()
		unsubscribe()
		sess.finish()
	}()
	if !sess.append(context.Background(), events.NewCustomEvent("fill-subscriber")) {
		t.Fatal("could not fill subscriber")
	}
	got := ask(callCtx, sess, nameQuestion(t))
	waitForStalledElicitationPublish(t, sess)

	type listResult struct {
		status int
		body   string
		err    error
	}
	listed := make(chan listResult, 1)
	go func() {
		resp, err := http.Get(baseURL + "/agent/runs/" + sess.RunID + "/elicitations")
		if err != nil {
			listed <- listResult{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		listed <- listResult{status: resp.StatusCode, body: string(body), err: err}
	}()
	var list listResult
	select {
	case list = <-listed:
	case <-time.After(2500 * time.Millisecond):
		endCall()
		unsubscribe()
		<-listed
		t.Fatal("GET open forms waited on a full subscriber past the delivery grace")
	}
	if list.err != nil || list.status != http.StatusOK || !strings.Contains(list.body, `"questions":[{`) {
		t.Fatalf("GET open forms = %d %q, %v", list.status, list.body, list.err)
	}
	open := sess.questions.open()
	if len(open) != 1 {
		t.Fatalf("open questions = %d, want one", len(open))
	}
	if sev := <-ch; sev.Seq != 1 {
		t.Fatalf("first subscriber frame = %d, want filler sequence 1", sev.Seq)
	}
	select {
	case _, stillOpen := <-ch:
		if stillOpen {
			t.Fatal("subscriber received a later frame after missing the question")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber remained attached after missing the question")
	}
	endCall()
	if r := result(t, got); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Ask = %+v, want call cancellation", r)
	}
	assertQuestionThenResolutionInReplay(t, sess, open[0].ID, elicit.ActionCancel)
}

func TestStalledResolutionCannotHoldAnswerOrRunFinish(t *testing.T) {
	baseURL, sess, ch, unsubscribe := stalledElicitationRun(t)
	runCtx, endRun := context.WithCancel(context.Background())
	sess.questions.bind(runCtx)
	defer func() {
		endRun()
		unsubscribe()
		sess.finish()
	}()
	got := ask(context.Background(), sess, nameQuestion(t))
	question := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	if !sess.append(context.Background(), events.NewCustomEvent("fill-subscriber")) {
		t.Fatal("could not fill subscriber")
	}

	type postResult struct {
		status int
		err    error
	}
	posted := make(chan postResult, 1)
	go func() {
		resp, err := http.Post(baseURL+answerURL(sess.RunID, question.ID), "application/json", strings.NewReader(`{"action":"decline"}`))
		if err != nil {
			posted <- postResult{err: err}
			return
		}
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, resp.Body)
		posted <- postResult{status: resp.StatusCode, err: err}
	}()
	waitForStalledElicitationPublish(t, sess)
	finished := make(chan struct{})
	go func() {
		sess.finish()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2500 * time.Millisecond):
		endRun()
		unsubscribe()
		<-finished
		t.Fatal("run finish waited on a full subscriber past the delivery grace")
	}
	if post := <-posted; post.err != nil || post.status != http.StatusAccepted {
		t.Fatalf("POST answer = %d, %v; want 202", post.status, post.err)
	}
	if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionDecline {
		t.Fatalf("Ask = %+v, want the posted decline", r)
	}
	assertQuestionThenResolutionInReplay(t, sess, question.ID, elicit.ActionDecline)
}

func TestAbortedQuestionFanoutRetiresSubscriberForOrderedReplay(t *testing.T) {
	sess := newRunSession("run-replay", "thread", localIdentityID, 8, 1, nil)
	ch, unsubscribe, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("fresh session cannot subscribe")
	}
	defer func() {
		unsubscribe()
		sess.finish()
	}()
	if !sess.append(context.Background(), events.NewCustomEvent("fill-subscriber")) {
		t.Fatal("could not fill subscriber")
	}
	q := elicit.Question{ID: "q-replay", Server: "forms", Tool: "ask_name", Message: "name?"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sess.publish(ctx, events.NewCustomEvent(ElicitationEventName, events.WithValue(elicitationFrame{RunID: sess.RunID, Question: q}))) {
		t.Fatal("full subscriber unexpectedly accepted a canceled question frame")
	}
	if sev := <-ch; sev.Seq != 1 {
		t.Fatalf("first subscriber frame = %d, want filler sequence 1", sev.Seq)
	}
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("subscriber received a frame after it missed the question")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber stayed attached after missing the question")
	}
	unsubscribe()
	if !sess.publish(context.Background(), events.NewCustomEvent(ElicitationResolvedEventName,
		events.WithValue(elicitationResolvedFrame{ID: q.ID, Action: elicit.ActionDecline}))) {
		t.Fatal("resolution could not be recorded after subscriber retirement")
	}
	assertQuestionThenResolutionInReplay(t, sess, q.ID, elicit.ActionDecline)
}
