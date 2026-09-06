package agui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/askuser"
)

const (
	resumeThread      = "11111111-1111-4111-8111-111111111111"
	resumeOtherThread = "22222222-2222-4222-8222-222222222222"
	resumeToken       = "01a0773d-3cf3-7b9e-a93e-2165e5b85735"
	resumeOtherToken  = "01a0773d-3cf3-7b9e-a93e-2165e5b85736"
)

func pendingFor(thread, toolCallID, token string) askuser.Pending {
	return askuser.Pending{Token: token, ConversationID: thread, ToolCallID: toolCallID}
}

func resumeServer(t *testing.T, pendings ...askuser.Pending) *Server {
	t.Helper()
	s := NewServer(nil, &errConvStore{}, ServerConfig{})
	s.SetApprovalStore(&fakeApprovalStore{pendings: pendings})
	return s
}

// The gateway publishes an interrupt's tool_call id on RUN_FINISHED and its resume path
// keys on the pause token, so a conforming client echoing back what it was given was
// answered `invalid token "c0"` (measured live 2026-09-06).
func TestResumeAcceptsThePublishedInterruptID(t *testing.T) {
	s := resumeServer(t, pendingFor(resumeThread, "c0", resumeToken))

	out, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: "c0", Status: types.ResumeStatusResolved}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].InterruptID != resumeToken {
		t.Fatalf("entries = %+v, want the tool call id resolved to its pause token", out)
	}
}

// A caller that already holds the token (the cockpit, via GET /api/approvals) must be
// untouched — the store is not even read.
func TestResumeLeavesATokenAlone(t *testing.T) {
	s := resumeServer(t, pendingFor(resumeThread, "c0", resumeToken))

	out, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: resumeToken}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].InterruptID != resumeToken {
		t.Fatalf("entry = %q, want it untouched", out[0].InterruptID)
	}
}

// A tool_call id is unique within a round, not across a conversation, so two still-pending
// pauses can share one. Answering whichever the store listed first would answer the wrong
// question.
func TestResumeRefusesAnAmbiguousToolCallID(t *testing.T) {
	s := resumeServer(t,
		pendingFor(resumeThread, "c0", resumeToken),
		pendingFor(resumeThread, "c0", resumeOtherToken),
	)

	_, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: "c0"}})
	if err == nil || !strings.Contains(err.Error(), "two pending pauses") {
		t.Fatalf("err = %v, want an ambiguity refusal", err)
	}
}

// A pause on ANOTHER thread never resolves this thread's id: the resume must not reach
// across conversations even within one identity.
func TestResumeDoesNotResolveAcrossThreads(t *testing.T) {
	s := resumeServer(t, pendingFor(resumeOtherThread, "c0", resumeToken))

	out, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: "c0"}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].InterruptID != "c0" {
		t.Fatalf("entry = %q, want the unresolvable id left for the token path to reject", out[0].InterruptID)
	}
}

// An unknown id is LEFT ALONE rather than rejected here, so the existing GetByToken path
// keeps producing the one error message for it.
func TestResumeLeavesAnUnknownIDToTheTokenPath(t *testing.T) {
	s := resumeServer(t)

	out, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].InterruptID != "nope" {
		t.Fatalf("entry = %q, want it untouched", out[0].InterruptID)
	}
}

// With no approval store wired the resume path behaves exactly as it did before.
func TestResumeWithoutAnApprovalStoreIsUnchanged(t *testing.T) {
	s := NewServer(nil, &errConvStore{}, ServerConfig{})

	out, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: "c0"}})
	if err != nil || out[0].InterruptID != "c0" {
		t.Fatalf("out = %+v err = %v, want the entry untouched", out, err)
	}
}

// A store failure is reported, never treated as "no pending pauses" — that would turn a
// readable outage into an invalid-token error naming the client's own correct id.
func TestResumeReportsAStoreFailure(t *testing.T) {
	s := NewServer(nil, &errConvStore{}, ServerConfig{})
	s.SetApprovalStore(&fakeApprovalStore{err: errors.New("store down")})

	if _, err := s.resolveInterruptIDs(context.Background(), resumeThread,
		[]types.ResumeEntry{{InterruptID: "c0"}}); err == nil ||
		!strings.Contains(err.Error(), "store down") {
		t.Fatalf("err = %v, want the store failure surfaced", err)
	}
}
