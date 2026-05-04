package sandbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

func TestNewManager_RequiresRuntime(t *testing.T) {
	_, err := sandbox.NewManager(sandbox.Config{})
	if err == nil || !strings.Contains(err.Error(), "runtime is required") {
		t.Fatalf("expected required-runtime error, got %v", err)
	}
}

func TestNewManager_RuntimeAdapter(t *testing.T) {
	runtime := &fakeRuntime{
		kind: sandbox.RuntimeKindPyodide,
		availability: sandbox.Availability{
			Available: true,
			Kind:      sandbox.RuntimeKindPyodide,
			Detail:    "fake pyodide ready",
		},
		result: &sandbox.Result{
			OK:        true,
			Stdout:    "hi\n",
			ExitCode:  0,
			ElapsedMs: 1,
		},
	}

	mgr, err := sandbox.NewManager(sandbox.Config{Runtime: runtime})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if got := mgr.RuntimeKind(); got != sandbox.RuntimeKindPyodide {
		t.Fatalf("RuntimeKind() = %q, want pyodide", got)
	}
	availability := mgr.CheckAvailability()
	if !availability.Available || availability.Kind != sandbox.RuntimeKindPyodide || availability.Detail != "fake pyodide ready" {
		t.Fatalf("availability = %+v", availability)
	}
	result, err := mgr.Execute(context.Background(), "print('hi')", true)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Stdout != "hi\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if runtime.executedCode != "print('hi')" || !runtime.executedAllowNetwork {
		t.Fatalf("runtime did not receive execute request: code=%q allow=%v", runtime.executedCode, runtime.executedAllowNetwork)
	}
}

func TestManager_CheckAvailabilityWithoutRuntime(t *testing.T) {
	availability := (*sandbox.Manager)(nil).CheckAvailability()
	if availability.Available {
		t.Fatal("nil manager should be unavailable")
	}
	if availability.Kind != sandbox.RuntimeKindUnavailable {
		t.Fatalf("kind = %q, want unavailable", availability.Kind)
	}
	if availability.Detail == "" {
		t.Fatal("detail should explain unavailable runtime")
	}
}

func TestValidateCode_RejectsEmptyAndOversize(t *testing.T) {
	mgr := newValidationManager(t, nil)

	if err := mgr.ValidateCode(" \n\t "); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty-code error, got %v", err)
	}

	largeCode := strings.Repeat("x", 100_001)
	if err := mgr.ValidateCode(largeCode); err == nil || !strings.Contains(err.Error(), "exceeds 100KB") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestValidateCode_DelegatesToRuntime(t *testing.T) {
	wantErr := errors.New("syntax error: line 1")
	mgr := newValidationManager(t, wantErr)

	err := mgr.ValidateCode("def broken(:\n    pass\n")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ValidateCode() = %v, want %v", err, wantErr)
	}
}

func newValidationManager(t *testing.T, validateErr error) *sandbox.Manager {
	t.Helper()
	mgr, err := sandbox.NewManager(sandbox.Config{
		Runtime: &fakeRuntime{
			kind:        sandbox.RuntimeKindPyodide,
			validateErr: validateErr,
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return mgr
}

type fakeRuntime struct {
	kind                 sandbox.RuntimeKind
	availability         sandbox.Availability
	result               *sandbox.Result
	validateErr          error
	executeErr           error
	executedCode         string
	executedAllowNetwork bool
}

func (r *fakeRuntime) Kind() sandbox.RuntimeKind {
	return r.kind
}

func (r *fakeRuntime) Execute(_ context.Context, code string, allowNetwork bool) (*sandbox.Result, error) {
	r.executedCode = code
	r.executedAllowNetwork = allowNetwork
	return r.result, r.executeErr
}

func (r *fakeRuntime) CheckAvailability() sandbox.Availability {
	return r.availability
}

func (r *fakeRuntime) ValidateCode(_ string) error {
	return r.validateErr
}
