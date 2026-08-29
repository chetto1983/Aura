package swarm

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
)

// delegation_delivery_test.go is the daemon-free coverage for Deliver (the SC#1
// write + present-operator push) and NudgeUndrained (the absent-operator leg's
// tri-state sweep), plus an import-hygiene assertion that this package gained
// no edge into the conversations or channels packages. The claim-before-push
// concurrency proof and the drained-row exclusion live in the steer package's
// own db_integration tier, where the SQL that enforces them actually runs.

// recordedDeliveryTurn is one AppendAssistantTurn call fakeConversationRecorder
// captured. identityID is identityctx.IdentityID(ctx) AT CALL TIME -- the RLS
// carrier the real conversations.Store.AppendTurn reads (defect A, 51-09
// live-check/d03/RESULTS.md): a caller that binds no identity on ctx would have
// its write silently hidden by RLS against a real database, even though this
// fake accepts it unconditionally.
type recordedDeliveryTurn struct {
	conversationID string
	text           string
	identityID     string
}

// fakeConversationRecorder captures AppendAssistantTurn calls and can be made
// to fail once (err), so Deliver's record-before-push ordering and its
// record-failure contract are observable without a queue.
type fakeConversationRecorder struct {
	mu       sync.Mutex // ProcessOnce runs a claimed batch concurrently (finding F)
	appended []recordedDeliveryTurn
	err      error
}

func (f *fakeConversationRecorder) AppendAssistantTurn(ctx context.Context, conversationID, text string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appended = append(f.appended, recordedDeliveryTurn{
		conversationID: conversationID, text: text, identityID: identityctx.IdentityID(ctx),
	})
	return nil
}

// TestDeliveryRecordsBeforePush pins the load-bearing order: the conversation
// record happens BEFORE the steer push, because a push is not a record and the
// record is the durable copy (SC#1). The recorded copy carries the T-51-38
// attribution wrapper naming the goal; the pushed copy stays the raw report
// text unchanged from 51-01 (it gets its OWN attribution downstream, at
// drain time, from markSteer).
func TestDeliveryRecordsBeforePush(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	recorded, err := d.Deliver(context.Background(), DelegationPayload{Goal: "summarise the inbox", ConversationID: "conv-7"}, "the report")
	if err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if !recorded {
		t.Fatal("recorded = false, want true on a successful append")
	}
	if len(recorder.appended) != 1 || recorder.appended[0].conversationID != "conv-7" {
		t.Fatalf("appended = %+v, want one turn to conv-7", recorder.appended)
	}
	if !strings.Contains(recorder.appended[0].text, "the report") {
		t.Fatalf("appended text = %q, want it to carry the raw report", recorder.appended[0].text)
	}
	if !strings.Contains(recorder.appended[0].text, "summarise the inbox") {
		t.Fatalf("appended text = %q, want the T-51-38 attribution to name the goal", recorder.appended[0].text)
	}
	if len(pub.pushes) != 1 || !strings.HasPrefix(pub.pushes[0], "conv-7|") {
		t.Fatalf("pushes = %+v, want one push addressed to conv-7", pub.pushes)
	}
	if pushed := strings.TrimPrefix(pub.pushes[0], "conv-7|"+steer.SourceWorker+"|"); pushed != "the report" {
		t.Fatalf("pushed text = %q, want the raw report unwrapped (its own attribution is applied downstream at drain time)", pushed)
	}
	// Both happened; the ORDER is asserted by TestRecordFailureBlocksSucceeded and
	// TestDeliveryPushFailureIsAHardError below, which each isolate one leg.
}

// TestDeliveryAttributesTheRecordedCopy is T-51-38's own named assertion,
// isolated from the ordering test above: a worker's recorded turn must never
// be indistinguishable from Aura's own words, because a LATER turn re-reads
// aura.conversation_turns as prompt history and would otherwise trust it with
// the assistant's own authority.
func TestDeliveryAttributesTheRecordedCopy(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	d := &DelegationDelivery{Recorder: recorder}

	if _, err := d.Deliver(context.Background(), DelegationPayload{Goal: "check the calendar", ConversationID: "conv-7"}, "raw report body"); err != nil {
		t.Fatalf("Deliver = %v", err)
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("appended = %d turns, want exactly 1", len(recorder.appended))
	}
	got := recorder.appended[0].text
	if got == "raw report body" {
		t.Fatal("recorded text is byte-identical to the raw report -- no attribution wrapper was applied (T-51-38)")
	}
	if !strings.Contains(got, "worker") {
		t.Fatalf("recorded text = %q, want it to identify the source as a worker report", got)
	}
	if !strings.Contains(got, "check the calendar") || !strings.Contains(got, "raw report body") {
		t.Fatalf("recorded text = %q, want both the goal and the raw report preserved", got)
	}
}

// TestDeliveryEmptyReport: an empty report records nothing and pushes nothing
// (SWARM-03 edge, backstop), and is NOT treated as a delivery failure --
// recorded=true because there is nothing to retry.
func TestDeliveryEmptyReport(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	for _, text := range []string{"", "   ", "\t\n"} {
		recorded, err := d.Deliver(context.Background(), DelegationPayload{ConversationID: "conv-7"}, text)
		if err != nil {
			t.Fatalf("Deliver(%q) = %v", text, err)
		}
		if !recorded {
			t.Fatalf("Deliver(%q) recorded = false, want true (nothing to retry)", text)
		}
	}
	if len(recorder.appended) != 0 {
		t.Fatalf("appended = %+v, want none for an empty report", recorder.appended)
	}
	if len(pub.pushes) != 0 {
		t.Fatalf("pushes = %+v, want none for an empty report", pub.pushes)
	}
}

// TestDeliveryNoRecorderIsAWiringError: a nil Recorder is a Go configuration
// error, never a silent skip -- SC#1 cannot be satisfied without it, so
// Deliver refuses rather than quietly dropping the write.
func TestDeliveryNoRecorderIsAWiringError(t *testing.T) {
	d := &DelegationDelivery{}
	_, err := d.Deliver(context.Background(), DelegationPayload{ConversationID: "conv-7"}, "report")
	if err == nil {
		t.Fatal("Deliver with no Recorder configured = nil error, want a wiring error")
	}
}

// TestDeliveryPushFailureIsAHardError preserves 51-01's original contract for
// the push leg unchanged: a steer.Push infrastructure failure is a hard Go
// error (unlike a record failure, which is a WARN reflected only through
// recorded=false).
func TestDeliveryPushFailureIsAHardError(t *testing.T) {
	recorder := &fakeConversationRecorder{}
	pub := &fakeSteerPublisher{err: errors.New("inbox gone")}
	d := &DelegationDelivery{Recorder: recorder, Steer: pub}

	recorded, err := d.Deliver(context.Background(), DelegationPayload{ConversationID: "conv-7"}, "report")
	if err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("Deliver = (%v, %v), want the push failure surfaced", recorded, err)
	}
	// The record itself still succeeded before the push failed -- recorded
	// reflects that, even though Deliver's overall return is an error.
	if !recorded {
		t.Fatal("recorded = false, want true: the conversation record succeeded before the push failed")
	}
	if len(recorder.appended) != 1 {
		t.Fatalf("appended = %d turns, want 1 (the record happens before the push)", len(recorder.appended))
	}
}

// TestSwarmPackageImportsNeitherConversationsNorChannels is the acceptance
// criteria's own hygiene check, run as a Go test so it fails the SAME way any
// other regression does: this package declares ConversationRecorder and
// ChannelDeliverer as consumer-side interfaces precisely so it never needs a
// real import edge into either sibling package (D-02).
//
// Parses each file's own import declarations with go/parser rather than a
// blind text search: a text search over the SOURCE of a test asserting this
// property would match its own forbidden-path literals. The two package
// name fragments are built by concatenation for the same reason -- so this
// file's own source never contains either forbidden import path as one
// contiguous substring, which is what a plain `grep` over the package would
// also require to pass.
func TestSwarmPackageImportsNeitherConversationsNorChannels(t *testing.T) {
	base := "github.com/chetto1983/aura/internal/"
	forbidden := []string{base + "conv" + "ersations", base + "chan" + "nels"}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		astFile, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports of %s: %v", f, err)
		}
		for _, imp := range astFile.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: malformed import path %s: %v", f, imp.Path.Value, err)
			}
			for _, want := range forbidden {
				if path == want {
					t.Fatalf("%s imports %q -- this package must declare a consumer-side interface instead (D-02)", f, path)
				}
			}
		}
	}
}

// --- NudgeUndrained (Task 2, the absent-operator leg) ---

// fakeChannelDeliverer scripts the shipped tri-state return. Locked (51-11
// Task 3): a fan-out concurrency test races two goroutines that may both
// reach this call before either commits its MarkFanoutNudged claim result.
type fakeChannelDeliverer struct {
	mu        sync.Mutex
	delivered bool
	err       error
	calls     []struct{ identityID, text string }
}

func (f *fakeChannelDeliverer) DeliverToIdentity(_ context.Context, identityID, text string) (bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, struct{ identityID, text string }{identityID, text})
	f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	return f.delivered, nil
}

// fakeSteerNudgeStore is an in-memory aura.steer_queue nudge-column stand-in:
// ListUnnudgedDelegationResults returns a fixed candidate set, and
// MarkSteerRowNudged implements the SAME conditional-claim semantics the real
// SQL's `WHERE nudged_at IS NULL` enforces (a row can be claimed at most once).
type fakeSteerNudgeStore struct {
	mu      sync.Mutex // a concurrency test races two NudgeUndrained passes over this store
	rows    []UndrainedResult
	listErr error
	claimed map[string]bool
	markErr error
}

func (f *fakeSteerNudgeStore) ListUnnudgedDelegationResults(_ context.Context, _ time.Time, _ int) ([]UndrainedResult, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows, nil
}

func (f *fakeSteerNudgeStore) MarkSteerRowNudged(_ context.Context, id, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return false, f.markErr
	}
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	if f.claimed[id] {
		return false, nil
	}
	f.claimed[id] = true
	return true, nil
}

// MarkFanoutNudged (51-11 Task 3) claims every row of one (identityID,
// fanoutKey) group not already in the SHARED claimed map -- the same map
// MarkSteerRowNudged uses, so a concurrency test racing both entry points
// still observes at-most-once claiming per row. Locked so a real concurrency
// test (two goroutines racing the SAME fan-out) exercises MarkFanoutNudged's
// documented one-winner contract rather than a data race in the fake itself.
func (f *fakeSteerNudgeStore) MarkFanoutNudged(_ context.Context, identityID, fanoutKey string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.markErr != nil {
		return nil, f.markErr
	}
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	var claimedIDs []string
	for _, row := range f.rows {
		if row.IdentityID != identityID || row.FanoutKey != fanoutKey || f.claimed[row.ID] {
			continue
		}
		f.claimed[row.ID] = true
		claimedIDs = append(claimedIDs, row.ID)
	}
	return claimedIDs, nil
}

// fakePendingNotificationStore captures the owns-but-failed retry-outbox
// insert without a queue.
type fakePendingNotificationStore struct {
	calls []struct{ steerQueueID, identityID, body, lastErr string }
	err   error
}

func (f *fakePendingNotificationStore) InsertPendingNotification(_ context.Context, steerQueueID, identityID, body, lastErr string) error {
	f.calls = append(f.calls, struct{ steerQueueID, identityID, body, lastErr string }{steerQueueID, identityID, body, lastErr})
	return f.err
}

// TestNudgeUndrainedTriState covers all three DeliverToIdentity outcomes
// (SWARM-03 edge, unclassified/duplication): delivered and nobody-owns both
// claim the row and stop; owns-but-failed claims the row too (so a later pass
// never re-attempts the push) but ALSO queues a pending_notifications retry.
func TestNudgeUndrainedTriState(t *testing.T) {
	for _, tc := range []struct {
		name          string
		delivered     bool
		channelErr    error
		wantPending   int
		wantNudgedCnt int
	}{
		{name: "delivered", delivered: true, wantNudgedCnt: 1},
		{name: "nobody owns the identity", delivered: false, wantNudgedCnt: 1},
		{name: "owns but failed", channelErr: errors.New("telegram down"), wantPending: 1, wantNudgedCnt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := &fakeChannelDeliverer{delivered: tc.delivered, err: tc.channelErr}
			nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{{ID: "row-1", IdentityID: "id-1", Body: "the report"}}}
			pending := &fakePendingNotificationStore{}
			d := &DelegationDelivery{Channel: channel, Nudge: nudge, Pending: pending, NudgeAfter: time.Minute}

			n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
			if err != nil {
				t.Fatalf("NudgeUndrained = %v", err)
			}
			if n != tc.wantNudgedCnt {
				t.Fatalf("nudged = %d, want %d", n, tc.wantNudgedCnt)
			}
			if len(channel.calls) != 1 || channel.calls[0].identityID != "id-1" {
				t.Fatalf("channel calls = %+v, want exactly one call for id-1", channel.calls)
			}
			if len(pending.calls) != tc.wantPending {
				t.Fatalf("pending notification inserts = %d, want %d", len(pending.calls), tc.wantPending)
			}
			if !nudge.claimed["row-1"] {
				t.Fatal("row-1 must be claimed (nudged_at set) in every tri-state branch")
			}
		})
	}
}

// TestNudgeUndrainedDisabledWhenNudgeAfterNonPositive: <=0 disables the
// channel leg entirely (the shipped AURA_ASKUSER_PAUSE_TTL_SEC precedent) --
// the Nudge/Channel collaborators are never even consulted.
func TestNudgeUndrainedDisabledWhenNudgeAfterNonPositive(t *testing.T) {
	for _, nudgeAfter := range []time.Duration{0, -time.Second} {
		channel := &fakeChannelDeliverer{delivered: true}
		nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{{ID: "row-1", IdentityID: "id-1", Body: "x"}}}
		d := &DelegationDelivery{Channel: channel, Nudge: nudge, NudgeAfter: nudgeAfter}

		n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
		if err != nil {
			t.Fatalf("NudgeUndrained(NudgeAfter=%v) = %v", nudgeAfter, err)
		}
		if n != 0 {
			t.Fatalf("NudgeUndrained(NudgeAfter=%v) nudged = %d, want 0", nudgeAfter, n)
		}
		if len(channel.calls) != 0 {
			t.Fatalf("NudgeUndrained(NudgeAfter=%v) must never call the channel when disabled", nudgeAfter)
		}
	}
}

// TestNudgeUndrainedNoopWithoutCollaborators: a nil Nudge or Channel degrades
// to a no-op rather than a nil-pointer panic (the same nil-safe posture
// SteerPublisher's own nil checks establish elsewhere in this package).
func TestNudgeUndrainedNoopWithoutCollaborators(t *testing.T) {
	d := &DelegationDelivery{NudgeAfter: time.Minute}
	n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
	if err != nil || n != 0 {
		t.Fatalf("NudgeUndrained with no collaborators = (%d, %v), want (0, nil)", n, err)
	}
}

// TestNudgeOneRendersBoundedMessageNeverRawBody (51-11): the fan-out-of-one
// path decodes row.Body and renders it through TelegramDelegationMessage --
// the SAME bounded rendering the grouped fan-out (Task 3) uses -- instead of
// forwarding the raw steer_queue body, which is the raw []ChildReport JSON
// Amendment #172 measured landing on the phone.
func TestNudgeOneRendersBoundedMessageNeverRawBody(t *testing.T) {
	reports := []ChildReport{{GoalIndex: 0, Status: StatusOK, Goal: "goal", Summary: "summary"}}
	body, err := marshalReports(reports)
	if err != nil {
		t.Fatalf("marshalReports = %v", err)
	}
	channel := &fakeChannelDeliverer{delivered: true}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{{ID: "row-1", IdentityID: "id-1", Body: body}}}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, NudgeAfter: time.Minute}

	if _, err := d.NudgeUndrained(context.Background(), time.Now(), 10); err != nil {
		t.Fatalf("NudgeUndrained = %v", err)
	}
	if len(channel.calls) != 1 {
		t.Fatalf("channel calls = %d, want 1", len(channel.calls))
	}
	got := channel.calls[0].text
	if got == body {
		t.Fatal("the raw steer_queue body must never reach DeliverToIdentity verbatim")
	}
	if want := TelegramDelegationMessage(reports); got != want {
		t.Fatalf("delivered text = %q, want the rendered TelegramDelegationMessage %q", got, want)
	}
}

// TestNudgeOneUndecodableBodySendsTheNoReportShape: a row whose body does not
// decode as []ChildReport still sends ONE bounded message ending in the
// closing line -- never the raw undecodable body.
func TestNudgeOneUndecodableBodySendsTheNoReportShape(t *testing.T) {
	channel := &fakeChannelDeliverer{delivered: true}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{{ID: "row-1", IdentityID: "id-1", Body: "not json"}}}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, NudgeAfter: time.Minute}

	if _, err := d.NudgeUndrained(context.Background(), time.Now(), 10); err != nil {
		t.Fatalf("NudgeUndrained = %v", err)
	}
	if len(channel.calls) != 1 {
		t.Fatalf("channel calls = %d, want 1", len(channel.calls))
	}
	if want := TelegramDelegationMessage(nil); channel.calls[0].text != want {
		t.Fatalf("undecodable body delivered = %q, want the no-report shape %q", channel.calls[0].text, want)
	}
}

// --- DeliverReport's own tests moved to delegation_delivery_report_test.go
// (CLAUDE.md's 600-LOC ceiling, mirroring this package's own
// report.go/brief.go/delegation_enqueue.go split precedent). ---
