package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeObservabilityChecker struct {
	err error
}

func (f *fakeObservabilityChecker) Check(context.Context) error { return f.err }

func TestObservabilityCheckMeta(t *testing.T) {
	m := NewObservabilityCheckHandler(nil).Meta()
	if m.Kind != KindObservabilityCheck || string(m.Kind) != "observability_check" {
		t.Fatalf("kind = %q", m.Kind)
	}
	if m.MaxDuration != observabilityCheckMaxDuration {
		t.Fatalf("max duration = %s", m.MaxDuration)
	}
	if m.ReschedulesOnRecovery {
		t.Fatal("periodic observability check must not catch up a missed run")
	}
}

func TestObservabilityCheckNotifiesOnlyTransitions(t *testing.T) {
	checker := &fakeObservabilityChecker{}
	h := NewObservabilityCheckHandler(checker)

	if summary, err := h.Run(context.Background(), Job{}); err != nil || summary != "" {
		t.Fatalf("initial healthy = (%q, %v)", summary, err)
	}

	checker.err = errors.New("scrape blind")
	if _, err := h.Run(context.Background(), Job{}); err == nil {
		t.Fatal("first failure returned nil")
	} else if suppressesNotification(err) {
		t.Fatal("first failure must alert")
	}

	if _, err := h.Run(context.Background(), Job{}); err == nil || !suppressesNotification(err) {
		t.Fatalf("repeated failure = %v, want suppressed error", err)
	}

	checker.err = nil
	summary, err := h.Run(context.Background(), Job{})
	if err != nil || !strings.Contains(summary, "recovered") {
		t.Fatalf("recovery = (%q, %v)", summary, err)
	}
	if summary, err := h.Run(context.Background(), Job{}); err != nil || summary != "" {
		t.Fatalf("steady healthy = (%q, %v)", summary, err)
	}
}

func TestObservabilityCheckDisabledIsSilent(t *testing.T) {
	summary, err := NewObservabilityCheckHandler(nil).Run(context.Background(), Job{})
	if err != nil || summary != "" {
		t.Fatalf("disabled = (%q, %v)", summary, err)
	}
}

func suppressesNotification(err error) bool {
	var suppressor interface{ SuppressSchedulerNotification() bool }
	return errors.As(err, &suppressor) && suppressor.SuppressSchedulerNotification()
}
