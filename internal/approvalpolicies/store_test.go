package approvalpolicies

import (
	"context"
	"errors"
	"testing"
)

// The validation that runs before any row is touched is pinned here, with no database: the
// vocabulary, the empty subject, and the malformed identity all fail before withIdentity.
func TestParsePolicyAdmitsOnlyTheTwoWords(t *testing.T) {
	for _, ok := range []string{"ask", "deny"} {
		if got, err := ParsePolicy(ok); err != nil || string(got) != ok {
			t.Errorf("ParsePolicy(%q) = %q, %v", ok, got, err)
		}
	}
	for _, bad := range []string{"", "maybe", "ASK", "allow"} {
		if _, err := ParsePolicy(bad); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("ParsePolicy(%q) = %v, want ErrInvalidPolicy", bad, err)
		}
	}
}

func TestSetRefusesWhatTheTableWouldRefuse(t *testing.T) {
	s := &Store{}
	const identity = "00000000-0000-0000-0000-000000000001"
	if err := s.Set(context.Background(), "not-a-uuid", "shell_exec", "", PolicyAsk, ""); err == nil {
		t.Error("a malformed identity must be refused")
	}
	if err := s.Set(context.Background(), identity, "", "", PolicyAsk, ""); err == nil {
		t.Error("an empty tool must be refused")
	}
	if err := s.Set(context.Background(), identity, "shell_exec", "", Policy("maybe"), ""); !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("an unknown policy word = %v, want ErrInvalidPolicy", err)
	}
	for _, call := range []func() error{
		func() error { _, _, err := s.Get(context.Background(), "x", "t", ""); return err },
		func() error { _, err := s.List(context.Background(), "x"); return err },
		func() error { _, err := s.Clear(context.Background(), "x", "t", ""); return err },
	} {
		if err := call(); err == nil {
			t.Error("a malformed identity must be refused before the store is touched")
		}
	}
}

func TestSubjectReadsLikeTheGrant(t *testing.T) {
	if got := (Row{Tool: "calendar", Action: "send_email"}).Subject(); got != "calendar send_email" {
		t.Errorf("subject = %q", got)
	}
	if got := (Row{Tool: "shell_exec"}).Subject(); got != "shell_exec" {
		t.Errorf("subject = %q", got)
	}
}
