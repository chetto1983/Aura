package messagedrafts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func mustFingerprint(t *testing.T, target Target, raw string) string {
	t.Helper()
	fp, err := Fingerprint(target, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Fingerprint(%s) = %v", raw, err)
	}
	return fp
}

func TestFingerprintBindsTargetAndEdits(t *testing.T) {
	first := mustFingerprint(t, emailSend, `{"to":["first@example.test"],"subject":"Hello"}`)
	second := mustFingerprint(t, emailSend, `{"to":["second@example.test"],"subject":"Hello"}`)
	otherTool := mustFingerprint(t, Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, `{"to":["first@example.test"],"subject":"Hello"}`)
	retired := mustFingerprint(t, Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}, `{"to":["first@example.test"],"subject":"Hello"}`)
	if len(first) != 64 || first == second || first == otherTool || first == retired {
		t.Fatal("draft fingerprint is not bound to tool and effective arguments")
	}
}

// Stored drafts keep the fingerprints they were made with, so the envelope the hash covers may
// not change: the WhatsApp rows were hashed with an empty "action" key.
func TestFingerprintHashesTheStoredEnvelope(t *testing.T) {
	args := `{"message":"m","recipient":"1"}`
	sum := sha256.Sum256([]byte(`{"recipe":"recipe:whatsapp","tool":"send_message","action":"","args":` + args + `}`))
	if got := mustFingerprint(t, Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, args); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %s, want the hash of the stored envelope", got)
	}
	if _, err := Fingerprint(emailSend, json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed arguments were fingerprinted")
	}
}

// Fingerprint hashes the bytes it is given; MergeApprovedArgs is what makes JSONB's reordered keys
// and the model's order the same bytes, so the pair is what a stored fingerprint is checked with.
func TestFingerprintIsStableAcrossKeyOrder(t *testing.T) {
	var fps []string
	for _, raw := range []string{
		`{"to":["first@example.test"],"subject":"Hello","body":"Hi","accountId":"t__personale"}`,
		`{"accountId":"t__personale","body":"Hi","subject":"Hello","to":["first@example.test"]}`,
	} {
		canonical, err := MergeApprovedArgs(emailSend, json.RawMessage(raw), nil)
		if err != nil {
			t.Fatal(err)
		}
		fps = append(fps, mustFingerprint(t, emailSend, string(canonical)))
	}
	if fps[0] != fps[1] {
		t.Fatalf("fingerprints differ by key order: %s, %s", fps[0], fps[1])
	}
}

func TestStoreRejectsInvalidDraftBeforeDatabase(t *testing.T) {
	store := NewStore(nil)
	valid := DraftInput{
		IdentityID: "11111111-1111-4111-8111-111111111111", ConversationID: "thread-1", ToolCallID: "call-1",
		Target:             Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"12345","message":"hello"}`), ExpiresAt: time.Now().Add(time.Hour),
	}
	cases := []DraftInput{
		{IdentityID: valid.IdentityID, Target: valid.Target, OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
		{IdentityID: "invalid", ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: valid.Target, OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
		{IdentityID: valid.IdentityID, ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: valid.Target, OriginalArgs: valid.OriginalArgs, ExpiresAt: time.Now().Add(-time.Minute)},
		{IdentityID: valid.IdentityID, ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: valid.Target, RegisteredToolName: "malformed:name", OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
		{IdentityID: valid.IdentityID, ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: Target{Recipe: "custom", Tool: "send_message"}, OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
	}
	for _, input := range cases {
		if _, err := store.Create(context.Background(), input); err == nil {
			t.Fatal("invalid draft reached the database")
		}
	}
	if _, err := store.MarkOutcome(context.Background(), valid.IdentityID, valid.ToolCallID, StatusPending, ""); err == nil {
		t.Fatal("pending is not a transport outcome")
	}
	if _, err := store.MarkOutcome(context.Background(), valid.IdentityID, valid.ToolCallID, StatusSent, "private content"); err == nil {
		t.Fatal("unsafe outcome code accepted")
	}
	if _, err := store.Get(context.Background(), valid.IdentityID, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("malformed draft ID was not treated as unavailable")
	}
	if _, err := store.ListPending(context.Background(), "invalid-owner", valid.ConversationID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid owner listed drafts")
	}
	if _, err := store.ClaimSend(context.Background(), valid.IdentityID, "not-a-uuid", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid draft ID reached the send claim")
	}
	if _, err := store.Decline(context.Background(), valid.IdentityID, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid draft ID reached decline")
	}
	if _, err := store.Reconcile(context.Background(), valid.IdentityID, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid draft ID reached recovery")
	}
}

func TestRegisteredToolNameAcceptsHashedBridgeName(t *testing.T) {
	if !validRegisteredToolName("namespace_that_is_very_long_and_needs_truncate__c_0123456789ab") {
		t.Fatal("a hash-suffixed bridge name was rejected")
	}
	for _, name := range []string{"short", "server__tool!", strings.Repeat("a", 55) + "__tool_name"} {
		if validRegisteredToolName(name) {
			t.Fatalf("invalid mounted tool name accepted: %q", name)
		}
	}
}

func TestClaimMatchesOnlyExactOwnerMountedToolAndArguments(t *testing.T) {
	target := emailSend
	args := json.RawMessage(`{"to":["first@example.test"],"subject":"Hello","body":"Reviewed"}`)
	canonical, err := MergeApprovedArgs(target, args, nil)
	if err != nil {
		t.Fatal(err)
	}
	claim := Draft{
		IdentityID: "owner", RegisteredToolName: "pim__send_email", Target: target,
		Status: StatusDispatching, EffectiveArgs: json.RawMessage(`{"body":"Reviewed","subject":"Hello","to":["first@example.test"]}`),
		EffectiveFingerprint: mustFingerprint(t, target, string(canonical)),
	}
	if !claim.MatchesDispatch("owner", "pim__send_email", target, args) {
		t.Fatal("claimed draft rejected identical effective arguments after JSONB reordering")
	}
	if claim.MatchesDispatch("other", "pim__send_email", target, args) ||
		claim.MatchesDispatch("owner", "other__send_email", target, args) ||
		claim.MatchesDispatch("owner", "pim__send_email", target, json.RawMessage(`{"to":["second@example.test"],"subject":"Hello","body":"Reviewed"}`)) {
		t.Fatal("claim authorized changed owner, tool, or destination")
	}
}

// A scheduled draft carries both its task and its time, the time is ahead, and the review
// outlives the send (migration 0155's CHECKs, refused before any query).
func TestCreateRefusesAHalfScheduledDraft(t *testing.T) {
	store := NewStore(nil)
	sendAt := time.Now().Add(time.Hour)
	base := DraftInput{
		IdentityID: "11111111-1111-4111-8111-111111111111", ConversationID: "thread-1", ToolCallID: "call-1",
		Target:             Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"10000000000","message":"hello"}`),
		ExpiresAt:          sendAt.Add(15 * time.Minute),
	}
	task := "22222222-2222-4222-8222-222222222222"
	cases := map[string]func(*DraftInput){
		"a time without a task": func(in *DraftInput) { in.SendAt = sendAt },
		"a task without a time": func(in *DraftInput) { in.ScheduledTaskID = task },
		"a time in the past": func(in *DraftInput) {
			in.ScheduledTaskID, in.SendAt = task, time.Now().Add(-time.Minute)
		},
		"a review that ends at the send": func(in *DraftInput) {
			in.ScheduledTaskID, in.SendAt, in.ExpiresAt = task, sendAt, sendAt
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := store.Create(context.Background(), input); !errors.Is(err, errInvalidDraftArgs) {
				t.Fatalf("err = %v, want errInvalidDraftArgs", err)
			}
		})
	}
}
