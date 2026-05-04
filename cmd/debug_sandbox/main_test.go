package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

func TestRunExecuteCodeToolSmokeUsesRegisteredToolBoundary(t *testing.T) {
	rt := &debugSandboxFakeRuntime{
		result: &sandbox.Result{
			OK:        true,
			Stdout:    "5050\n",
			ExitCode:  0,
			ElapsedMs: 12,
		},
	}

	report := runExecuteCodeToolSmoke(context.Background(), rt)

	if !report.OK {
		t.Fatalf("report.OK = false, error=%q", report.Error)
	}
	if !strings.Contains(rt.code, "sum(range(1, 101))") {
		t.Fatalf("executed code = %q, want sum(range(1, 101)) smoke", rt.code)
	}
	if !strings.Contains(report.Output, "5050") {
		t.Fatalf("output = %q, want 5050", report.Output)
	}
}

func TestRunExecuteCodeToolSmokeFailsWhenRuntimeUnavailable(t *testing.T) {
	rt := &debugSandboxFakeRuntime{
		availability: sandbox.Availability{
			Available: false,
			Kind:      sandbox.RuntimeKindPyodide,
			Detail:    "missing runner",
		},
	}

	report := runExecuteCodeToolSmoke(context.Background(), rt)

	if report.OK {
		t.Fatal("report.OK = true, want false")
	}
	if !strings.Contains(report.Error, "missing runner") {
		t.Fatalf("error = %q, want availability diagnostic", report.Error)
	}
}

func TestRunExecuteCodeArtifactSmokeRequiresArtifact(t *testing.T) {
	rt := &debugSandboxFakeRuntime{
		result: &sandbox.Result{
			OK:        true,
			Stdout:    "wrote sales artifacts\n",
			ExitCode:  0,
			ElapsedMs: 12,
			Artifacts: []sandbox.Artifact{
				{
					Name:      "aura_sales_summary.csv",
					MimeType:  "text/csv",
					Bytes:     []byte("month,revenue\nJan,120\n"),
					SizeBytes: int64(len("month,revenue\nJan,120\n")),
				},
				{
					Name:      "aura_sales_plot.png",
					MimeType:  "image/png",
					Bytes:     []byte("png-bytes"),
					SizeBytes: int64(len("png-bytes")),
				},
			},
		},
	}

	report := runExecuteCodeArtifactSmoke(context.Background(), rt)

	if !report.OK {
		t.Fatalf("report.OK = false, error=%q", report.Error)
	}
	if !strings.Contains(rt.code, "pandas") || !strings.Contains(rt.code, "matplotlib") {
		t.Fatalf("executed code = %q, want pandas + matplotlib smoke", rt.code)
	}
	if !strings.Contains(report.Output, "aura_sales_summary.csv") || !strings.Contains(report.Output, "aura_sales_plot.png") {
		t.Fatalf("output = %q, want rich artifact metadata", report.Output)
	}
	if strings.Count(report.Output, "persisted=true") != 2 || strings.Count(report.Output, "source_id=src_") != 2 {
		t.Fatalf("output = %q, want persisted source metadata", report.Output)
	}
}

type debugSandboxFakeRuntime struct {
	availability sandbox.Availability
	result       *sandbox.Result
	err          error
	code         string
}

func (r *debugSandboxFakeRuntime) Kind() sandbox.RuntimeKind {
	return sandbox.RuntimeKindPyodide
}

func (r *debugSandboxFakeRuntime) Execute(_ context.Context, code string, _ bool) (*sandbox.Result, error) {
	r.code = code
	if r.err != nil {
		return nil, r.err
	}
	if r.result == nil {
		return nil, errors.New("missing fake result")
	}
	return r.result, nil
}

func (r *debugSandboxFakeRuntime) CheckAvailability() sandbox.Availability {
	if r.availability.Detail != "" {
		return r.availability
	}
	return sandbox.Availability{
		Available: true,
		Kind:      sandbox.RuntimeKindPyodide,
		Detail:    "fake runtime available",
	}
}

func (*debugSandboxFakeRuntime) ValidateCode(string) error { return nil }
