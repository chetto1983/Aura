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

// delegation_delivery_report_test.go is DeliverReport's own daemon-free coverage
// (51-11 Task 1, extended by Task 3's fan-out key assertion) -- split out of
// delegation_delivery_test.go (which also carries Deliver and NudgeUndrained,
// CLAUDE.md's 600-LOC ceiling) rather than grown inline, mirroring this
// package's own report.go/brief.go/delegation_enqueue.go concern-split
// precedent. Shares delegation_delivery_test.go's fakeConversationRecorder and
// fakeSteerPublisher unchanged.

// archiverFunc adapts a plain func to ReportArchiver, so each test scripts
// exactly the archive behaviour it needs without a dedicated struct.
type archiverFunc func(ctx context.Context, identityID, conversationID, filename, markdown string) (string, error)

func (f archiverFunc) ArchiveReport(ctx context.Context, identityID, conversationID, filename, markdown string) (string, error) {
	return f(ctx, identityID, conversationID, filename, markdown)
}

// TestDeliverReportArchivesRecordsThenPushes pins DeliverReport's own
// load-bearing order: archive (so the card's artifact line has a filename to
// point to), THEN record (the durable copy), THEN push (the courtesy) --
// mirroring TestDeliveryRecordsBeforePush's record-before-push assertion for
// Deliver, extended one step earlier.
func TestDeliverReportArchivesRecordsThenPushes(t *testing.T) {
	var order []string
	archiver := archiverFunc(func(context.Context, string, string, string, string) (string, error) {
		order = append(order, "archive")
		return "asset-1", nil
	})
	recorder := &orderedConversationRecorder{order: &order}
	pub := &orderedSteerPublisher{order: &order}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub, Archiver: archiver}

	report := ChildReport{ChildID: "w1-abc", Status: StatusOK, Goal: "goal", Summary: "summary"}
	recorded, err := d.DeliverReport(context.Background(), DelegationPayload{ConversationID: "conv-9", FanoutKey: "f-test"}, report, 90*time.Second)
	if err != nil {
		t.Fatalf("DeliverReport = %v", err)
	}
	if !recorded {
		t.Fatal("recorded = false, want true")
	}
	want := []string{"archive", "record", "push"}
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
// slice so archive-then-record-then-push is asserted as ONE sequence, not
// three independent side effects that could still interleave.
type orderedConversationRecorder struct {
	order    *[]string
	lastText string
}

func (r *orderedConversationRecorder) AppendAssistantTurn(_ context.Context, _, text string) error {
	*r.order = append(*r.order, "record")
	r.lastText = text
	return nil
}

type orderedSteerPublisher struct{ order *[]string }

func (p *orderedSteerPublisher) Push(_, _, _ string) error {
	*p.order = append(*p.order, "push")
	return nil
}

func (p *orderedSteerPublisher) PushDelegationResult(_, _, _, _ string) error {
	*p.order = append(*p.order, "push")
	return nil
}

// TestDeliverReportBoundsSteerCopyButArchivesFullReport is Amendment #178's
// measured regression: a model can return more than steer's 32 KiB message
// cap even though the full Markdown archive succeeds. The courtesy steer
// copy must remain structured and useful without making the completed job
// retry solely because its report is long.
func TestDeliverReportBoundsSteerCopyButArchivesFullReport(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{maxBytes: 32768}
	var archivedMarkdown string
	archiver := archiverFunc(func(_ context.Context, _, _, _, markdown string) (string, error) {
		archivedMarkdown = markdown
		return "asset-1", nil
	})
	d := &DelegationDelivery{Recorder: recorder, Steer: pub, Archiver: archiver}

	fullSummary := strings.Repeat("long worker result with quoted JSON: {\"ok\":true}\n", 2500)
	report := ChildReport{
		GoalIndex: 1,
		ChildID:   "w2",
		Status:    StatusOK,
		Goal:      "produce a detailed report",
		Summary:   fullSummary,
		Attempts:  2,
	}
	if _, err := d.DeliverReport(context.Background(), DelegationPayload{ConversationID: "conv-3", FanoutKey: "f-test"}, report, 30*time.Second); err != nil {
		t.Fatalf("DeliverReport = %v", err)
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
	if !strings.Contains(archivedMarkdown, fullSummary) {
		t.Fatal("archived Markdown must retain the complete uncapped summary")
	}
}

// TestDeliverReportWritesTheFanoutKey (51-11 Task 3, acceptance criteria's own
// named check): DeliverReport is the ONLY producer of a non-empty fanout_key
// -- this asserts the fake publisher's recorded key equals payload.FanoutKey
// by value, the assertion this task's own action text calls "the step that
// makes the whole task real". Without it every aura.steer_queue.fanout_key
// would violate the explicit fan-out invariant and make grouped delivery impossible
// regardless of how many workers a swarm_spawn call
// dispatched -- the exact defect D-15/Amendment #172 point 2 forbid.
func TestDeliverReportWritesTheFanoutKey(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	report := ChildReport{ChildID: "w2-abc", Status: StatusOK, Goal: "goal", Summary: "summary"}
	payload := DelegationPayload{ConversationID: "conv-fanout", FanoutKey: "f-deadbeefcafef00d"}
	if _, err := d.DeliverReport(context.Background(), payload, report, time.Minute); err != nil {
		t.Fatalf("DeliverReport = %v", err)
	}
	if len(pub.fanoutKeys) != 1 {
		t.Fatalf("fanoutKeys = %d, want 1", len(pub.fanoutKeys))
	}
	if pub.fanoutKeys[0] != payload.FanoutKey {
		t.Fatalf("pushed fanout key = %q, want payload.FanoutKey %q", pub.fanoutKeys[0], payload.FanoutKey)
	}
}

// TestDeliverReportNilArchiverDegradesToNoArtifactPointer: a nil Archiver
// (a pool-less boot, matching newDelegationDelivery's other nil-safe
// collaborators) must never fail the delivery -- the card simply carries no
// artifact line.
func TestDeliverReportNilArchiverDegradesToNoArtifactPointer(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	d := &DelegationDelivery{Recorder: recorder}
	report := ChildReport{ChildID: "w1", Status: StatusOK, Goal: "goal", Summary: "summary"}
	recorded, err := d.DeliverReport(context.Background(), DelegationPayload{ConversationID: "conv-4", FanoutKey: "f-test"}, report, time.Minute)
	if err != nil || !recorded {
		t.Fatalf("DeliverReport = (%v, %v), want (true, nil)", recorded, err)
	}
	if strings.Contains(recorder.appended[0].text, "Report completo") {
		t.Fatalf("card must carry no artifact line when Archiver is nil: %q", recorder.appended[0].text)
	}
}

// TestDeliverReportArchiveErrorDegradesToNoArtifactPointer: an ArchiveReport
// error is a best-effort WARN, never a delivery failure -- a Garage/object-
// store hiccup must not block SC#1's own write.
func TestDeliverReportArchiveErrorDegradesToNoArtifactPointer(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	archiver := archiverFunc(func(context.Context, string, string, string, string) (string, error) {
		return "", errors.New("garage down")
	})
	d := &DelegationDelivery{Recorder: recorder, Archiver: archiver}
	report := ChildReport{ChildID: "w1", Status: StatusOK, Goal: "goal", Summary: "summary"}
	recorded, err := d.DeliverReport(context.Background(), DelegationPayload{ConversationID: "conv-5", FanoutKey: "f-test"}, report, time.Minute)
	if err != nil || !recorded {
		t.Fatalf("DeliverReport = (%v, %v), want (true, nil): an archive failure must never fail delivery", recorded, err)
	}
	if strings.Contains(recorder.appended[0].text, "Report completo") {
		t.Fatal("card must carry no artifact line when the archive call errors")
	}
}

// TestDeliverReportRecordFailureReturnsFalse mirrors Deliver's own record-
// failure contract: a WARN, never a hard Go error, reflected solely through
// recorded=false so the caller's succeeded transition stays gated.
func TestDeliverReportRecordFailureReturnsFalse(t *testing.T) {
	recorder := &fakeConversationRecorder{err: errors.New("db down")}
	d := &DelegationDelivery{Recorder: recorder}
	report := ChildReport{ChildID: "w1", Status: StatusOK, Goal: "goal", Summary: "summary"}
	recorded, err := d.DeliverReport(context.Background(), DelegationPayload{ConversationID: "conv-6", FanoutKey: "f-test"}, report, time.Minute)
	if err != nil {
		t.Fatalf("DeliverReport = %v, want no hard error on a record failure", err)
	}
	if recorded {
		t.Fatal("recorded = true, want false on a conversation-append failure")
	}
}

// TestDeliverReportNoRecorderIsAWiringError mirrors Deliver's own guard: SC#1
// cannot be satisfied without a Recorder, so DeliverReport refuses rather
// than silently dropping the write.
func TestDeliverReportNoRecorderIsAWiringError(t *testing.T) {
	d := &DelegationDelivery{}
	_, err := d.DeliverReport(context.Background(), DelegationPayload{ConversationID: "conv-1"}, ChildReport{Status: StatusOK}, time.Minute)
	if err == nil {
		t.Fatal("DeliverReport with no Recorder configured = nil error, want a wiring error")
	}
}
