package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/steer"
)

// delegation_delivery_report_test.go is DeliverPreparedReport's daemon-free coverage
// (51-11 Task 1, extended by Task 3's fan-out key assertion) -- split out of
// delegation_delivery_test.go (which also carries Deliver and NudgeUndrained,
// CLAUDE.md's 600-LOC ceiling) rather than grown inline, mirroring this
// package's own report.go/brief.go/delegation_enqueue.go concern-split
// precedent. Shares delegation_delivery_test.go's fakeConversationRecorder and
// fakeSteerPublisher unchanged.

// archiverFunc adapts a plain func to ReportArchiver, so each test scripts
// exactly the archive behaviour it needs without a dedicated struct.
type archiverFunc func(ctx context.Context, identityID, conversationID, deliveryKey, filename, markdown string) (string, error)

func (f archiverFunc) ArchiveReport(ctx context.Context, identityID, conversationID, deliveryKey, filename, markdown string) (string, error) {
	return f(ctx, identityID, conversationID, deliveryKey, filename, markdown)
}

func successfulReportArchiver() ReportArchiver {
	return archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		return "asset-1", nil
	})
}

// TestDeliverPreparedReportRecordsThenPushes pins the terminal delivery's own
// load-bearing order: record (the durable copy) THEN push (the courtesy) --
// mirroring TestDeliveryRecordsBeforePush for Deliver. The archive already ran
// (deliverPending), so the card names the artifact it was handed.
func TestDeliverPreparedReportRecordsThenPushes(t *testing.T) {
	var order []string
	recorder := &orderedConversationRecorder{order: &order}
	pub := &orderedSteerPublisher{order: &order}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	report := ChildReport{ChildID: "w1-abc", Status: StatusOK, Goal: "goal", Summary: "summary"}
	recorded, err := d.DeliverPreparedReport(context.Background(), DelegationPayload{ConversationID: "conv-9", FanoutKey: "f-test"}, report, 90*time.Second, "w1-abc.md", "dk-1")
	if err != nil {
		t.Fatalf("DeliverPreparedReport = %v", err)
	}
	if !recorded {
		t.Fatal("recorded = false, want true")
	}
	want := []string{"record", "push"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if !strings.Contains(recorder.lastText, "w1-abc.md") {
		t.Fatalf("recorded card = %q, want the archived filename named", recorder.lastText)
	}
}

// orderedConversationRecorder/orderedSteerPublisher append to a shared *order
// slice so record-then-push is asserted as ONE sequence, not two independent
// side effects that could still interleave.
type orderedConversationRecorder struct {
	order    *[]string
	lastText string
}

func (r *orderedConversationRecorder) AppendAssistantTurn(_ context.Context, _, text string) error {
	*r.order = append(*r.order, "record")
	r.lastText = text
	return nil
}

func (r *orderedConversationRecorder) AppendAssistantTurnIdempotent(ctx context.Context, conversationID, _, text string) error {
	return r.AppendAssistantTurn(ctx, conversationID, text)
}

type orderedSteerPublisher struct{ order *[]string }

func (p *orderedSteerPublisher) Push(_, _, _ string) error {
	*p.order = append(*p.order, "push")
	return nil
}

func (p *orderedSteerPublisher) PushDelegationResultIdempotent(_, _, _, _, _ string) error {
	*p.order = append(*p.order, "push")
	return nil
}

// TestDeliverPreparedReportBoundsSteerCopy is Amendment #178's measured
// regression: a model can return more than steer's 32 KiB message cap even
// though the full Markdown archive succeeds. The courtesy steer copy must
// remain structured and useful without making the completed job retry solely
// because its report is long.
func TestDeliverPreparedReportBoundsSteerCopy(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{maxBytes: 32768}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	fullSummary := strings.Repeat("long worker result with quoted JSON: {\"ok\":true}\n", 2500)
	report := ChildReport{
		GoalIndex: 1,
		ChildID:   "w2",
		Status:    StatusOK,
		Goal:      "produce a detailed report",
		Summary:   fullSummary,
		Attempts:  2,
	}
	if _, err := d.DeliverPreparedReport(context.Background(), DelegationPayload{ConversationID: "conv-3", FanoutKey: "f-test"}, report, 30*time.Second, "w2.md", "dk-1"); err != nil {
		t.Fatalf("DeliverPreparedReport = %v", err)
	}
	if len(pub.pushes) != 1 {
		t.Fatalf("pushes = %d, want 1", len(pub.pushes))
	}
	pushed := strings.TrimPrefix(pub.pushes[0], "conv-3|"+steer.SourceWorker+"|")
	if len([]byte(pushed)) > pub.maxBytes {
		t.Fatalf("pushed bytes = %d, want <= %d", len([]byte(pushed)), pub.maxBytes)
	}
	var reports []ChildReport
	if err := json.Unmarshal([]byte(pushed), &reports); err != nil {
		t.Fatalf("pushed text is not ChildReport JSON: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("pushed reports = %d, want 1", len(reports))
	}
	got := reports[0]
	if got.GoalIndex != report.GoalIndex || got.ChildID != report.ChildID || got.Status != report.Status || got.Attempts != report.Attempts {
		t.Fatalf("pushed report identity = %+v, want structural fields from %+v", got, report)
	}
	if got.Summary == fullSummary || !strings.HasSuffix(got.Summary, "…") {
		t.Fatalf("pushed summary must be rune-bounded with an ellipsis, got %d bytes", len([]byte(got.Summary)))
	}
}

// TestDeliverPreparedReportWritesTheFanoutKey (51-11 Task 3, acceptance criteria's own
// named check): the terminal delivery is the ONLY producer of a non-empty fanout_key
// -- this asserts the fake publisher's recorded key equals payload.FanoutKey
// by value, the assertion this task's own action text calls "the step that
// makes the whole task real". Without it every aura.steer_queue.fanout_key
// would violate the explicit fan-out invariant and make grouped delivery impossible
// regardless of how many workers a swarm_spawn call
// dispatched -- the exact defect D-15/Amendment #172 point 2 forbid.
func TestDeliverPreparedReportWritesTheFanoutKey(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	report := ChildReport{ChildID: "w2-abc", Status: StatusOK, Goal: "goal", Summary: "summary"}
	payload := DelegationPayload{ConversationID: "conv-fanout", FanoutKey: "f-deadbeefcafef00d"}
	if _, err := d.DeliverPreparedReport(context.Background(), payload, report, time.Minute, "w2-abc.md", "dk-1"); err != nil {
		t.Fatalf("DeliverPreparedReport = %v", err)
	}
	if len(pub.fanoutKeys) != 1 {
		t.Fatalf("fanoutKeys = %d, want 1", len(pub.fanoutKeys))
	}
	if pub.fanoutKeys[0] != payload.FanoutKey {
		t.Fatalf("pushed fanout key = %q, want payload.FanoutKey %q", pub.fanoutKeys[0], payload.FanoutKey)
	}
}

// TestDeliverPreparedReportRecordFailureReturnsFalse mirrors Deliver's own record-
// failure contract: a WARN, never a hard Go error, reflected solely through
// recorded=false so the caller's succeeded transition stays gated.
func TestDeliverPreparedReportRecordFailureReturnsFalse(t *testing.T) {
	recorder := &fakeConversationRecorder{err: errors.New("db down")}
	d := &DelegationDelivery{Recorder: recorder}
	report := ChildReport{ChildID: "w1", Status: StatusOK, Goal: "goal", Summary: "summary"}
	recorded, err := d.DeliverPreparedReport(context.Background(), DelegationPayload{ConversationID: "conv-6", FanoutKey: "f-test"}, report, time.Minute, "w1.md", "dk-1")
	if err != nil {
		t.Fatalf("DeliverPreparedReport = %v, want no hard error on a record failure", err)
	}
	if recorded {
		t.Fatal("recorded = true, want false on a conversation-append failure")
	}
}

// TestDeliverPreparedReportNoRecorderIsAWiringError mirrors Deliver's own guard:
// SC#1 cannot be satisfied without a Recorder, so the delivery refuses rather
// than silently dropping the write.
func TestDeliverPreparedReportNoRecorderIsAWiringError(t *testing.T) {
	d := &DelegationDelivery{}
	_, err := d.DeliverPreparedReport(context.Background(), DelegationPayload{ConversationID: "conv-1"}, ChildReport{Status: StatusOK}, time.Minute, "", "dk-1")
	if err == nil {
		t.Fatal("DeliverPreparedReport with no Recorder configured = nil error, want a wiring error")
	}
}

// TestDeliverPreparedReportRequiresADeliveryKey: the key is what makes the staged
// delivery idempotent across retries, so a delivery without one is refused before
// anything is recorded.
func TestDeliverPreparedReportRequiresADeliveryKey(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	d := &DelegationDelivery{Recorder: recorder}
	_, err := d.DeliverPreparedReport(context.Background(), DelegationPayload{ConversationID: "conv-1", FanoutKey: "f-test"}, ChildReport{Status: StatusOK}, time.Minute, "", " ")
	if err == nil {
		t.Fatal("DeliverPreparedReport with a blank delivery key = nil error, want a refusal")
	}
	if len(recorder.appended) != 0 {
		t.Fatal("a refused delivery must record nothing")
	}
}
