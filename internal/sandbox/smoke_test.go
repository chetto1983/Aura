package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunPyodideSmokeReportsUnavailableRuntime(t *testing.T) {
	rt := &smokeFakeRuntime{
		availability: Availability{
			Available: false,
			Kind:      RuntimeKindPyodide,
			Detail:    "manifest missing",
		},
	}

	report := RunPyodideSmoke(context.Background(), rt)

	if report.OK {
		t.Fatal("OK = true, want false")
	}
	if report.Availability.Available {
		t.Fatal("Availability.Available = true, want false")
	}
	if !strings.Contains(report.Error, "manifest missing") {
		t.Fatalf("Error = %q, want manifest diagnostic", report.Error)
	}
	if len(report.Scenarios) != 0 {
		t.Fatalf("Scenarios = %d, want 0 when runtime is unavailable", len(report.Scenarios))
	}
	if len(rt.calls) != 0 {
		t.Fatalf("Execute calls = %d, want 0", len(rt.calls))
	}
}

func TestRunPyodideSmokeCoversOfflinePackageScenarios(t *testing.T) {
	rt := &smokeFakeRuntime{
		availability: Availability{
			Available: true,
			Kind:      RuntimeKindPyodide,
			Detail:    "available",
		},
		results: map[string]*Result{
			"arithmetic":          {OK: true, Stdout: "AURA_SMOKE arithmetic ok\n", ExitCode: 0},
			"data_imports":        {OK: true, Stdout: "AURA_SMOKE data_imports ok\n", ExitCode: 0},
			"spreadsheet_read":    {OK: true, Stdout: "AURA_SMOKE spreadsheet_read ok\n", ExitCode: 0},
			"matplotlib_artifact": {OK: true, Stdout: "AURA_SMOKE matplotlib_artifact ok\n", ExitCode: 0},
			"pdf_text_extraction": {OK: true, Stdout: "AURA_SMOKE pdf_text_extraction ok\n", ExitCode: 0},
		},
	}

	report := RunPyodideSmoke(context.Background(), rt)

	if !report.OK {
		t.Fatalf("OK = false, report = %+v", report)
	}
	wantNames := []string{
		"arithmetic",
		"data_imports",
		"spreadsheet_read",
		"matplotlib_artifact",
		"pdf_text_extraction",
	}
	if len(report.Scenarios) != len(wantNames) {
		t.Fatalf("Scenarios = %d, want %d", len(report.Scenarios), len(wantNames))
	}
	for i, want := range wantNames {
		got := report.Scenarios[i]
		if got.Name != want || !got.OK {
			t.Fatalf("scenario %d = %+v, want %s ok", i, got, want)
		}
	}
	if len(rt.calls) != len(wantNames) {
		t.Fatalf("Execute calls = %d, want %d", len(rt.calls), len(wantNames))
	}
	for _, call := range rt.calls {
		if call.allowNetwork {
			t.Fatalf("%s allowNetwork = true, want false", call.name)
		}
	}
	assertSmokeCodeContains(t, rt.codeFor("data_imports"), "import numpy", "import pandas", "import scipy", "import pyarrow")
	assertSmokeCodeContains(t, rt.codeFor("spreadsheet_read"), "python_calamine", "load_workbook", "smoke.xlsx")
	assertSmokeCodeContains(t, rt.codeFor("matplotlib_artifact"), "matplotlib", "savefig", "plot.png")
	assertSmokeCodeContains(t, rt.codeFor("pdf_text_extraction"), "fitz", "BeautifulSoup", "PDF smoke marker")
}

func TestRunPyodideSmokeFailsWhenExpectedMarkerIsMissing(t *testing.T) {
	rt := &smokeFakeRuntime{
		availability: Availability{
			Available: true,
			Kind:      RuntimeKindPyodide,
			Detail:    "available",
		},
		results: map[string]*Result{
			"arithmetic": {OK: true, Stdout: "wrong marker\n", ExitCode: 0},
		},
	}

	report := RunPyodideSmoke(context.Background(), rt)

	if report.OK {
		t.Fatal("OK = true, want false")
	}
	if len(report.Scenarios) == 0 || report.Scenarios[0].OK {
		t.Fatalf("first scenario = %+v, want failed marker check", report.Scenarios)
	}
	if !strings.Contains(report.Scenarios[0].Error, "missing marker") {
		t.Fatalf("Error = %q, want missing marker", report.Scenarios[0].Error)
	}
}

type smokeFakeRuntime struct {
	availability Availability
	results      map[string]*Result
	calls        []smokeFakeCall
}

type smokeFakeCall struct {
	name         string
	code         string
	allowNetwork bool
}

func (r *smokeFakeRuntime) Kind() RuntimeKind { return RuntimeKindPyodide }

func (r *smokeFakeRuntime) CheckAvailability() Availability { return r.availability }

func (r *smokeFakeRuntime) ValidateCode(string) error { return nil }

func (r *smokeFakeRuntime) Execute(_ context.Context, code string, allowNetwork bool) (*Result, error) {
	name := ""
	for _, scenario := range PyodideSmokeScenarios() {
		if strings.Contains(code, scenario.Marker) {
			name = scenario.Name
			break
		}
	}
	if name == "" {
		return nil, errors.New("unknown smoke scenario")
	}
	r.calls = append(r.calls, smokeFakeCall{name: name, code: code, allowNetwork: allowNetwork})
	result := r.results[name]
	if result == nil {
		return &Result{OK: false, Stderr: "missing fake result", ExitCode: 1, ElapsedMs: int(time.Millisecond.Milliseconds())}, nil
	}
	return result, nil
}

func (r *smokeFakeRuntime) codeFor(name string) string {
	for _, call := range r.calls {
		if call.name == name {
			return call.code
		}
	}
	return ""
}

func assertSmokeCodeContains(t *testing.T, code string, want ...string) {
	t.Helper()
	if code == "" {
		t.Fatal("code is empty")
	}
	for _, needle := range want {
		if !strings.Contains(code, needle) {
			t.Fatalf("code missing %q:\n%s", needle, code)
		}
	}
}
