package agui

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
)

// run_elicitation.go is a detached run's elicit.Asker. A mounted MCP server's form
// is published into the run's own stream, and the answer arrives on POST
// /agent/runs/{runID}/elicitations/{id} (server_run_elicitation.go). A reload gets
// an open form back from the replay ring, or, once the ring has rotated past it,
// from GET /agent/runs/{runID}/elicitations. Nothing is persisted: the server's
// request does not survive a restart either.

// The CUSTOM events a question and its outcome travel as. Neither carries an
// answer's values: only the action reaches the stream.
const (
	ElicitationEventName         = "aura.elicitation"
	ElicitationResolvedEventName = "aura.elicitation_resolved"
	elicitationDeliveryTimeout   = time.Second
)

// elicitationFrame is the question as the cockpit receives it. run_id rides along
// because the answer is posted to the run, and a card restored from a replay has
// no other place to read it from.
type elicitationFrame struct {
	RunID string `json:"run_id"`
	elicit.Question
}

type elicitationResolvedFrame struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	// Expired tells an expiry from a cancel for another reason: both are a cancel to
	// the server, and only the card needs the difference.
	Expired bool `json:"expired,omitempty"`
}

var (
	errQuestionUnknown = errors.New("question not found")
	errQuestionClosed  = errors.New("question already resolved")
)

type pendingQuestion struct {
	q      elicit.Question
	order  uint64
	answer chan elicit.Answer // buffered: the closer never waits for Ask
}

// runQuestions holds a run's open questions. mu also orders the stream: a question
// is published under it and so is its resolution, so a resolution can never reach
// a subscriber before the question it closes, and cancelAll, which runs before the
// session turns terminal, waits for any close already publishing.
type runQuestions struct {
	sess *RunSession

	mu sync.Mutex
	// runCtx bounds the resolution frames. A resolution must still reach the stream
	// after the call that asked has ended, and must give up once the run has, so a
	// tab that stops reading cannot hold mu past the run's own cap.
	runCtx    context.Context
	pending   map[string]*pendingQuestion
	closed    map[string]struct{}
	ended     bool
	nextOrder uint64
}

func newRunQuestions(sess *RunSession) *runQuestions {
	return &runQuestions{
		sess: sess, runCtx: context.Background(),
		pending: map[string]*pendingQuestion{}, closed: map[string]struct{}{},
	}
}

// bind ties the run's resolution frames to runCtx and returns the asker to install
// on it. A session built without bind, as the tests build one, uses Background.
func (rq *runQuestions) bind(runCtx context.Context) *runQuestions {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	rq.runCtx = runCtx
	return rq
}

// Ask implements elicit.Asker. It returns when the operator answers, when ctx ends
// (with context.Cause(ctx)), or at once for a refusal, a run that has ended, or a
// run already showing elicit.MaxOpenQuestions forms. An answer delivered as ctx
// ends wins: it was already published as the outcome.
func (rq *runQuestions) Ask(ctx context.Context, q elicit.Question) (elicit.Answer, error) {
	if q.Refusal != "" {
		rq.NoticeRefusal(ctx, q)
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	q.ID = uuid.NewString()
	p := &pendingQuestion{q: q, answer: make(chan elicit.Answer, 1)}
	rq.mu.Lock()
	if rq.ended {
		rq.mu.Unlock()
		return elicit.Answer{Action: elicit.ActionCancel}, nil
	}
	// Each session is capped on its own (mcptools), so a run that reaches several
	// servers is capped here. Past it nothing is shown: a card per request would be
	// the flood itself.
	if len(rq.pending) >= elicit.MaxOpenQuestions {
		rq.mu.Unlock()
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	p.order = rq.nextOrder
	rq.nextOrder++
	rq.pending[q.ID] = p
	rq.publish(ctx, events.NewCustomEvent(ElicitationEventName, events.WithValue(rq.frame(q))))
	rq.mu.Unlock()

	select {
	case a := <-p.answer:
		return a, nil
	case <-ctx.Done():
		cause := context.Cause(ctx)
		if !rq.close(q.ID, elicit.ActionCancel, errors.Is(cause, elicit.ErrExpired)) {
			return <-p.answer, nil
		}
		return elicit.Answer{}, cause
	}
}

// NoticeRefusal records a bare question and its decline before returning. It
// never opens an answerable question; a concurrent POST can only see it as closed.
func (rq *runQuestions) NoticeRefusal(ctx context.Context, q elicit.Question) {
	if q.Refusal == "" {
		return
	}
	notice := elicit.Question{ID: uuid.NewString(), Server: q.Server, Tool: q.Tool, Refusal: q.Refusal}
	rq.mu.Lock()
	defer rq.mu.Unlock()
	if rq.ended {
		return
	}
	rq.closed[notice.ID] = struct{}{}
	rq.publish(ctx, events.NewCustomEvent(ElicitationEventName, events.WithValue(rq.frame(notice))))
	rq.publish(ctx, events.NewCustomEvent(ElicitationResolvedEventName,
		events.WithValue(elicitationResolvedFrame{ID: notice.ID, Action: elicit.ActionDecline})))
}

func (rq *runQuestions) frame(q elicit.Question) elicitationFrame {
	return elicitationFrame{RunID: rq.sess.RunID, Question: q}
}

// The ring records the frame before live fanout. A stalled viewer must not keep
// the question lock until the call or detached run reaches its outer deadline.
func (rq *runQuestions) publish(ctx context.Context, ev events.Event) {
	deliveryCtx, cancel := context.WithTimeout(ctx, elicitationDeliveryTimeout)
	defer cancel()
	rq.sess.publish(deliveryCtx, ev)
}

// Arrival order is independent of each call's deadline, including refusal notices.
func (rq *runQuestions) open() []elicitationFrame {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	frames := make([]elicitationFrame, 0, len(rq.pending))
	for _, p := range rq.pending {
		frames = append(frames, rq.frame(p.q))
	}
	slices.SortFunc(frames, func(a, b elicitationFrame) int {
		return cmp.Compare(rq.pending[a.ID].order, rq.pending[b.ID].order)
	})
	return frames
}

// answer delivers the operator's answer. An accept that fails the server's schema
// leaves the question open and returns the problem codes.
func (rq *runQuestions) answer(id string, a elicit.Answer) (elicit.FieldErrors, error) {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	p, ok := rq.pending[id]
	if !ok {
		if _, done := rq.closed[id]; done {
			return nil, errQuestionClosed
		}
		return nil, errQuestionUnknown
	}
	if a.Action == elicit.ActionAccept {
		if errs := elicit.Validate(p.q, a.Content); errs != nil {
			return errs, nil
		}
	} else {
		a.Content = nil
	}
	rq.closeLocked(id, a.Action, false)
	p.answer <- a
	return nil, nil
}

// cancelAll resolves every pending question as cancel and refuses new ones: a form
// never outlives its run. Session finish calls it before closing subscribers.
func (rq *runQuestions) cancelAll() {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	rq.ended = true
	for id, p := range rq.pending {
		rq.closeLocked(id, elicit.ActionCancel, false)
		p.answer <- elicit.Answer{Action: elicit.ActionCancel}
	}
}

// close resolves a question once, whoever gets there first, and reports whether
// this call was the one.
func (rq *runQuestions) close(id, action string, expired bool) bool {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return rq.closeLocked(id, action, expired)
}

func (rq *runQuestions) closeLocked(id, action string, expired bool) bool {
	if _, ok := rq.pending[id]; !ok {
		return false
	}
	delete(rq.pending, id)
	rq.closed[id] = struct{}{}
	rq.publish(rq.runCtx, events.NewCustomEvent(ElicitationResolvedEventName,
		events.WithValue(elicitationResolvedFrame{ID: id, Action: action, Expired: expired})))
	return true
}
