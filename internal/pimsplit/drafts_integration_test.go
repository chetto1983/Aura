//go:build db_integration

package pimsplit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// sendArgs carries what a byte-level fingerprint can trip on: several recipients, HTML that
// json.Marshal escapes, and an attachment object, all stored through jsonb. The attachment's keys
// are in jsonb's order on purpose: Create's original fingerprint hashes the caller's nested key
// order, which jsonb does not keep, so with any other order only that fingerprint differs (measured
// 2026-11-24). The effective fingerprint, which a send is checked against, is rebuilt from stored
// arguments on both sides and matches either way.
const sendArgs = `{"accountId":"acc-1","to":["a@example.com","b@example.com"],"cc":["c@example.com"],` +
	`"subject":"Hi <b>&","body":"<p>Hello & <b>bye</b></p>","bodyFormat":"html",` +
	`"attachments":[{"name":"a.txt","contentType":"text/plain","base64Content":"aGk="}]}`

func legacyArgs(t *testing.T, args string) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		t.Fatal(err)
	}
	fields["action"] = json.RawMessage(`"send_email"`)
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

type legacyDraft struct {
	status, registered, original, effective string
	sendAt                                  time.Time
}

// legacyDraft stores a draft in the multiplexed shape, as an Aura from before the split wrote it.
func (h *harness) legacyDraft(identity string, d legacyDraft) string {
	h.t.Helper()
	if d.registered == "" {
		d.registered = oldTool
	}
	var effective, effectiveFP, dispatchStarted, resolved, sendAt any
	if d.effective != "" {
		effective, effectiveFP = d.effective, strings.Repeat("b", 64)
	}
	now := time.Now()
	switch d.status {
	case "dispatching":
		dispatchStarted = now
	case "sent":
		dispatchStarted, resolved = now, now
	case "declined":
		resolved = now
	}
	if !d.sendAt.IsZero() {
		sendAt = d.sendAt
	}
	id := uuid.NewString()
	h.exec(`INSERT INTO aura.message_drafts
(id, identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
 original_args, original_fingerprint, effective_args, effective_fingerprint, status, expires_at,
 dispatch_started_at, resolved_at, send_at, sender_address)
VALUES ($1, $2, 'conv-legacy', $12, 'recipe:calendar', 'calendar', $3, 'send_email',
 $4, repeat('a', 64), $5, $6, $7, $8, $9, $10, $11, 'me@example.com')`,
		id, identity, d.registered, d.original, effective, effectiveFP, d.status, now.Add(2*time.Hour),
		dispatchStarted, resolved, sendAt, id)
	return id
}

type storedDraft struct {
	tool, action, registered, status string
	original, effective              []byte
	originalFP                       string
	effectiveFP, outcome             pgtype.Text
	resolved                         pgtype.Timestamptz
}

func (h *harness) readDraft(id string) storedDraft {
	h.t.Helper()
	var d storedDraft
	err := h.conn.QueryRow(h.ctx, `SELECT tool_name, action, registered_tool_name, status, original_args,
effective_args, original_fingerprint, effective_fingerprint, outcome_code, resolved_at
FROM aura.message_drafts WHERE id = $1`, id).Scan(&d.tool, &d.action, &d.registered, &d.status, &d.original,
		&d.effective, &d.originalFP, &d.effectiveFP, &d.outcome, &d.resolved)
	if err != nil {
		h.t.Fatalf("read draft: %v", err)
	}
	return d
}

func (h *harness) store() *messagedrafts.Store { return messagedrafts.NewStore(h.app) }

func (h *harness) sameJSON(a, b []byte) bool {
	h.t.Helper()
	var same bool
	if err := h.conn.QueryRow(h.ctx, `SELECT $1::jsonb = $2::jsonb`, string(a), string(b)).Scan(&same); err != nil {
		h.t.Fatalf("compare json: %v", err)
	}
	return same
}

func assertSendEmailShape(t *testing.T, d storedDraft) {
	t.Helper()
	if d.tool != "send_email" || d.action != "" || d.registered != mcptools.NamespacedName("calendar", "send_email") {
		t.Fatalf("target = %s/%q as %s", d.tool, d.action, d.registered)
	}
	if strings.Contains(string(d.original), `"action"`) || strings.Contains(string(d.effective), `"action"`) {
		t.Fatalf("action survived in the args: %s %s", d.original, d.effective)
	}
}

func TestPendingDraftWithAccountIsRewritten(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	store := h.store()
	created, err := store.Create(h.ctx, messagedrafts.DraftInput{
		IdentityID: id, ConversationID: "conv-new", ToolCallID: "call-new", Target: messagedrafts.EmailTarget(),
		RegisteredToolName: mcptools.NamespacedName("calendar", "send_email"),
		OriginalArgs:       json.RawMessage(sendArgs), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	legacy := h.legacyDraft(id, legacyDraft{status: "pending", original: legacyArgs(t, sendArgs)})
	if report := h.upgrade(); report != (Report{DraftsRewritten: 1}) {
		t.Fatalf("Report = %+v", report)
	}
	d := h.readDraft(legacy)
	assertSendEmailShape(t, d)
	if d.status != "pending" || d.effective != nil || d.effectiveFP.Valid {
		t.Fatalf("status %s, effective %s %v", d.status, d.effective, d.effectiveFP)
	}
	if !h.sameJSON(d.original, created.OriginalArgs) || d.originalFP != created.OriginalFingerprint {
		t.Fatalf("original %s / %s, Create stored %s / %s", d.original, d.originalFP, created.OriginalArgs, created.OriginalFingerprint)
	}
	claimed, err := store.ClaimSend(h.ctx, id, legacy, nil)
	if err != nil {
		t.Fatalf("the rewritten draft cannot be sent: %v", err)
	}
	if !claimed.MatchesDispatch(id, d.registered, messagedrafts.EmailTarget(), claimed.EffectiveArgs) {
		t.Fatal("the claimed send does not match its own draft")
	}
}

func TestApprovedScheduledDraftWithAccountIsRewritten(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	store := h.store()
	sendAt := time.Now().Add(time.Hour)
	task := uuid.NewString()
	h.exec(`INSERT INTO aura.scheduler_tasks (id, kind, schedule_kind, notify_route, identity_id, status)
VALUES ($1, 'message', 'at', 'none', $2, 'pending_approval')`, task, id)
	created, err := store.Create(h.ctx, messagedrafts.DraftInput{
		IdentityID: id, ConversationID: "conv-new", ToolCallID: "call-new", Target: messagedrafts.EmailTarget(),
		RegisteredToolName: mcptools.NamespacedName("calendar", "send_email"),
		OriginalArgs:       json.RawMessage(sendArgs), ExpiresAt: sendAt.Add(time.Hour),
		ScheduledTaskID: task, SendAt: sendAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	approved, err := store.Approve(h.ctx, id, created.ID, json.RawMessage(`{"subject":"Edited <i>"}`))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	legacy := h.legacyDraft(id, legacyDraft{status: "approved", sendAt: sendAt,
		original: legacyArgs(t, sendArgs), effective: legacyArgs(t, string(approved.EffectiveArgs))})
	if report := h.upgrade(); report != (Report{DraftsRewritten: 1}) {
		t.Fatalf("Report = %+v", report)
	}
	d := h.readDraft(legacy)
	assertSendEmailShape(t, d)
	if d.status != "approved" || d.originalFP != created.OriginalFingerprint ||
		d.effectiveFP.String != approved.EffectiveFingerprint || !h.sameJSON(d.effective, approved.EffectiveArgs) {
		t.Fatalf("status %s, fingerprints %s %s, Create and Approve gave %s %s",
			d.status, d.originalFP, d.effectiveFP.String, created.OriginalFingerprint, approved.EffectiveFingerprint)
	}
}

func TestDraftWithoutAccountIsExpired(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	unaccounted := `{"to": ["a@example.com"], "action": "send_email", "subject": "Hi"}`
	pending := h.legacyDraft(id, legacyDraft{status: "pending", original: unaccounted})
	approved := h.legacyDraft(id, legacyDraft{status: "approved", sendAt: time.Now().Add(time.Hour),
		original: unaccounted, effective: unaccounted})
	if report := h.upgrade(); report != (Report{DraftsExpired: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	for _, draft := range []string{pending, approved} {
		d := h.readDraft(draft)
		if d.status != "expired" || d.outcome.String != "account_required" || !d.resolved.Valid {
			t.Fatalf("draft = %+v", d)
		}
		if d.tool != "calendar" || d.action != "send_email" {
			t.Fatalf("an expired draft was rewritten: %+v", d)
		}
	}
}

// A draft whose server is no longer a PIM mount, or whose arguments the new send refuses, cannot be
// sent after the split; it is closed, and the log names it by id and never by its content.
func TestUnmappableDraftIsExpired(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	gone := h.legacyDraft(id, legacyDraft{status: "pending", registered: "gone__calendar", original: legacyArgs(t, sendArgs)})
	refused := h.legacyDraft(id, legacyDraft{status: "pending",
		original: `{"to": ["a@example.com"], "action": "send_email", "replyTo": "x", "subject": "Secret plan", "accountId": "acc-1"}`})
	if report := h.upgrade(); report != (Report{DraftsExpired: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	for _, draft := range []string{gone, refused} {
		if d := h.readDraft(draft); d.status != "expired" || d.outcome.String != "tool_split_unmappable" || !d.resolved.Valid {
			t.Fatalf("draft = %+v", d)
		}
		if !strings.Contains(h.logs.String(), draft) {
			t.Fatalf("the log does not name draft %s:\n%s", draft, h.logs.String())
		}
	}
	if logs := h.logs.String(); strings.Contains(logs, "Secret plan") || strings.Contains(logs, "a@example.com") {
		t.Fatalf("the log carries draft content:\n%s", logs)
	}
}

// Whatever a send in flight did stays the truth: its draft is neither rewritten nor closed.
func TestDispatchingDraftIsUntouched(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.legacyDraft(id, legacyDraft{status: "dispatching", original: `{"to": ["a@example.com"], "action": "send_email"}`,
		effective: `{"to": ["a@example.com"], "action": "send_email"}`})
	before := h.snapshot(id)
	if report := h.upgrade(); report != (Report{}) {
		t.Fatalf("Report = %+v", report)
	}
	if after := h.snapshot(id); after != before {
		t.Fatalf("the dispatching draft changed:\nbefore %s\nafter  %s", before, after)
	}
}

// A resolved draft is history: it keeps its multiplexed shape, and its review still reads it whole.
func TestResolvedDraftKeepsItsOldShape(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	args := legacyArgs(t, sendArgs)
	sent := h.legacyDraft(id, legacyDraft{status: "sent", original: args, effective: args})
	h.legacyDraft(id, legacyDraft{status: "declined", original: args})
	before := h.snapshot(id)
	if report := h.upgrade(); report != (Report{}) {
		t.Fatalf("Report = %+v", report)
	}
	if after := h.snapshot(id); after != before {
		t.Fatalf("a resolved draft changed:\nbefore %s\nafter  %s", before, after)
	}
	draft, err := h.store().Get(h.ctx, id, sent)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if draft.Target != messagedrafts.LegacyEmailTarget() {
		t.Fatalf("target = %+v", draft.Target)
	}
	if _, subject, _ := draft.Review(); subject != "Hi <b>&" {
		t.Fatalf("subject = %q", subject)
	}
	if name, address := draft.Sender(); name == "" || address != "me@example.com" {
		t.Fatalf("sender = %q %q", name, address)
	}
}
