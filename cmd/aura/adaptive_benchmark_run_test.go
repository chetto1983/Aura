package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestParseAdaptiveBenchmarkRunArgsAcceptsExactModeMatrix(t *testing.T) {
	t.Parallel()
	ownerID := uuid.Must(uuid.NewV7())
	outputDir := filepath.Join(t.TempDir(), "report")
	tests := []struct {
		name string
		args []string
		mode auraeval.AdaptiveBenchmarkMode
	}{
		{
			name: "qwen portability",
			args: []string{
				"--mode", "qwen-portability",
				"--owner-id", ownerID.String(),
				"--output-dir", outputDir,
				"--confirm-temporary-settings-override",
			},
			mode: auraeval.AdaptiveBenchmarkModeQwenPortability,
		},
		{
			name: "production shadow",
			args: []string{
				"--owner-id=" + ownerID.String(),
				"--output-dir=" + outputDir,
				"--mode=production-shadow",
			},
			mode: auraeval.AdaptiveBenchmarkModeProductionShadow,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request, err := parseAdaptiveBenchmarkRunArgs(testCase.args)
			if err != nil {
				t.Fatalf("parseAdaptiveBenchmarkRunArgs: %v", err)
			}
			if request.OwnerID != ownerID ||
				request.OutputDir != outputDir ||
				request.Mode != testCase.mode {
				t.Fatalf("request = %#v", request)
			}
			wantConfirmation :=
				testCase.mode == auraeval.AdaptiveBenchmarkModeQwenPortability
			if request.ConfirmedSettingsOverride != wantConfirmation {
				t.Fatalf(
					"confirmation = %t, want %t",
					request.ConfirmedSettingsOverride,
					wantConfirmation,
				)
			}
		})
	}
}

func TestParseAdaptiveBenchmarkRunArgsRejectsBeforeExecution(t *testing.T) {
	t.Parallel()
	ownerID := uuid.Must(uuid.NewV7()).String()
	outputDir := filepath.Join(t.TempDir(), "report")
	validQwen := []string{
		"--mode", "qwen-portability",
		"--owner-id", ownerID,
		"--output-dir", outputDir,
		"--confirm-temporary-settings-override",
	}
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing mode", args: validQwen[2:]},
		{name: "missing owner", args: validQwen[:2]},
		{name: "missing output", args: validQwen[:4]},
		{name: "unknown mode", args: replaceBenchmarkArg(validQwen, 1, "other")},
		{name: "noncanonical owner", args: replaceBenchmarkArg(validQwen, 3, "NOT-A-UUID")},
		{name: "nil owner", args: replaceBenchmarkArg(validQwen, 3, uuid.Nil.String())},
		{name: "relative output", args: replaceBenchmarkArg(validQwen, 5, "relative")},
		{
			name: "noncanonical output",
			args: replaceBenchmarkArg(
				validQwen,
				5,
				outputDir+string(filepath.Separator)+".."+
					string(filepath.Separator)+"report",
			),
		},
		{name: "unknown flag", args: appendCopy(validQwen, "--unknown")},
		{name: "positional", args: appendCopy(validQwen, "extra")},
		{name: "single dash", args: replaceBenchmarkArg(validQwen, 0, "-mode")},
		{name: "duplicate mode", args: appendCopy(validQwen, "--mode=qwen-portability")},
		{name: "duplicate owner", args: appendCopy(validQwen, "--owner-id="+ownerID)},
		{name: "duplicate output", args: appendCopy(validQwen, "--output-dir="+outputDir)},
		{
			name: "duplicate confirmation",
			args: appendCopy(validQwen, "--confirm-temporary-settings-override"),
		},
		{
			name: "missing qwen confirmation",
			args: validQwen[:len(validQwen)-1],
		},
		{
			name: "production confirmation",
			args: []string{
				"--mode", "production-shadow",
				"--owner-id", ownerID,
				"--output-dir", outputDir,
				"--confirm-temporary-settings-override",
			},
		},
		{
			name: "confirmation value",
			args: replaceBenchmarkArg(
				validQwen,
				len(validQwen)-1,
				"--confirm-temporary-settings-override=true",
			),
		},
		{
			name: "flag used as value",
			args: []string{
				"--mode", "--owner-id", ownerID,
				"--output-dir", outputDir,
			},
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if request, err := parseAdaptiveBenchmarkRunArgs(testCase.args); err == nil {
				t.Fatalf("accepted request %#v", request)
			}
		})
	}
}

func TestRunAdaptiveBenchmarkCommandParsesBeforeFactory(t *testing.T) {
	t.Parallel()
	factoryCalls := 0
	err := runAdaptiveBenchmarkRunCommand(
		t.Context(),
		[]string{"--mode", "unknown"},
		&bytes.Buffer{},
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			factoryCalls++
			return nil, nil
		},
	)
	if err == nil || factoryCalls != 0 {
		t.Fatalf("error=%v factory_calls=%d", err, factoryCalls)
	}
}

func TestRunAdaptiveBenchmarkCommandWritesCanonicalPayloadAndCloses(t *testing.T) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "production-shadow")
	execution := &adaptiveBenchmarkRunExecutionFake{
		payload: []byte(`{"schema_id":"aura.adaptive.benchmark-report/v1"}`),
	}
	var output bytes.Buffer

	err := runAdaptiveBenchmarkRunCommand(
		t.Context(),
		adaptiveBenchmarkRunArgs(request),
		&output,
		func(
			_ context.Context,
			got adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			if got != request {
				t.Fatalf("factory request = %#v, want %#v", got, request)
			}
			return execution, nil
		},
	)
	if err != nil {
		t.Fatalf("runAdaptiveBenchmarkRunCommand: %v", err)
	}
	if output.String() != string(execution.payload)+"\n" ||
		execution.runCalls != 1 ||
		execution.closeCalls != 1 ||
		execution.finalizeCalls != 1 ||
		!slices.Equal(
			execution.events,
			[]string{"run", "close", "finalize"},
		) {
		t.Fatalf(
			"output=%q run=%d close=%d finalize=%d events=%v",
			output.String(),
			execution.runCalls,
			execution.closeCalls,
			execution.finalizeCalls,
			execution.events,
		)
	}
	written, err := os.ReadFile(
		filepath.Join(request.OutputDir, adaptiveBenchmarkReportFilename),
	)
	if err != nil || string(written) != string(execution.payload)+"\n" {
		t.Fatalf("written report=%q error=%v", written, err)
	}
}

func TestRunAdaptiveBenchmarkCommandAlwaysClosesAndJoinsFailures(t *testing.T) {
	t.Parallel()
	runErr := errors.New("run failed")
	closeErr := errors.New("restore failed")
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	execution := &adaptiveBenchmarkRunExecutionFake{
		payload:  []byte(`{"should_not_publish":true}`),
		runErr:   runErr,
		closeErr: closeErr,
	}
	var output bytes.Buffer

	err := runAdaptiveBenchmarkRunCommand(
		t.Context(),
		adaptiveBenchmarkRunArgs(request),
		&output,
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			return execution, nil
		},
	)
	if !errors.Is(err, runErr) ||
		!errors.Is(err, closeErr) ||
		execution.closeCalls != 1 ||
		execution.finalizeCalls != 0 ||
		output.Len() != 0 {
		t.Fatalf(
			"error=%v close_calls=%d finalize_calls=%d output=%q",
			err,
			execution.closeCalls,
			execution.finalizeCalls,
			output.String(),
		)
	}
	if _, statErr := os.Stat(
		filepath.Join(request.OutputDir, adaptiveBenchmarkReportFilename),
	); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("report published after failed close: %v", statErr)
	}
}

func TestRunAdaptiveBenchmarkCommandDoesNotPublishAfterSuccessfulRunAndFailedClose(
	t *testing.T,
) {
	t.Parallel()
	closeErr := errors.New("exact restore failed")
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	execution := &adaptiveBenchmarkRunExecutionFake{
		payload:  []byte(`{"verdict":"pass"}`),
		closeErr: closeErr,
	}
	var output bytes.Buffer
	err := runAdaptiveBenchmarkRunCommand(
		t.Context(),
		adaptiveBenchmarkRunArgs(request),
		&output,
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			return execution, nil
		},
	)
	if !errors.Is(err, closeErr) ||
		execution.finalizeCalls != 0 ||
		output.Len() != 0 {
		t.Fatalf(
			"error=%v finalize_calls=%d output=%q",
			err,
			execution.finalizeCalls,
			output.String(),
		)
	}
	if _, statErr := os.Stat(
		filepath.Join(request.OutputDir, adaptiveBenchmarkReportFilename),
	); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("report published after failed restore: %v", statErr)
	}
}

func TestRunAdaptiveBenchmarkCommandPublishesOnlyAfterCloseAndSurfacesWriteFailure(
	t *testing.T,
) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "production-shadow")
	if err := os.MkdirAll(request.OutputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(
		request.OutputDir,
		adaptiveBenchmarkReportFilename,
	)
	if err := os.WriteFile(reportPath, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	execution := &adaptiveBenchmarkRunExecutionFake{
		payload: []byte(`{"new":true}`),
	}
	var output bytes.Buffer
	err := runAdaptiveBenchmarkRunCommand(
		t.Context(),
		adaptiveBenchmarkRunArgs(request),
		&output,
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			return execution, nil
		},
	)
	if err == nil ||
		execution.closeCalls != 1 ||
		execution.finalizeCalls != 1 ||
		output.Len() != 0 {
		t.Fatalf(
			"error=%v close_calls=%d finalize_calls=%d output=%q",
			err,
			execution.closeCalls,
			execution.finalizeCalls,
			output.String(),
		)
	}
	unchanged, readErr := os.ReadFile(reportPath)
	if readErr != nil || string(unchanged) != "existing\n" {
		t.Fatalf("existing report=%q error=%v", unchanged, readErr)
	}
}

func TestRunAdaptiveBenchmarkCommandRejectsShortStdoutWriteAfterPublication(
	t *testing.T,
) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "production-shadow")
	execution := &adaptiveBenchmarkRunExecutionFake{
		payload: []byte(`{"ok":true}`),
	}
	err := runAdaptiveBenchmarkRunCommand(
		t.Context(),
		adaptiveBenchmarkRunArgs(request),
		adaptiveBenchmarkShortWriter{},
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			return execution, nil
		},
	)
	if !errors.Is(err, io.ErrShortWrite) ||
		execution.closeCalls != 1 ||
		execution.finalizeCalls != 1 {
		t.Fatalf(
			"error=%v close_calls=%d finalize_calls=%d",
			err,
			execution.closeCalls,
			execution.finalizeCalls,
		)
	}
	if _, statErr := os.Stat(
		filepath.Join(request.OutputDir, adaptiveBenchmarkReportFilename),
	); statErr != nil {
		t.Fatalf("published report unavailable: %v", statErr)
	}
}

func TestRunEvalCommandDispatchesBenchmarkAfterStrictParsing(t *testing.T) {
	t.Parallel()
	factoryCalls := 0
	request := validAdaptiveBenchmarkRunRequest(t, "production-shadow")
	var output bytes.Buffer
	err := runEvalCommandWithBenchmarkFactory(
		t.Context(),
		append([]string{"adaptive", "benchmark"}, adaptiveBenchmarkRunArgs(request)...),
		&output,
		func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
			t.Fatal("cohort factory called for benchmark")
			return nil, nil, nil
		},
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			factoryCalls++
			return &adaptiveBenchmarkRunExecutionFake{payload: []byte("{}")}, nil
		},
	)
	if err != nil || factoryCalls != 1 || output.String() != "{}\n" {
		t.Fatalf(
			"error=%v factory_calls=%d output=%q",
			err,
			factoryCalls,
			output.String(),
		)
	}
}

func replaceBenchmarkArg(args []string, index int, value string) []string {
	replaced := append([]string(nil), args...)
	replaced[index] = value
	return replaced
}

func appendCopy(args []string, values ...string) []string {
	return append(append([]string(nil), args...), values...)
}

func validAdaptiveBenchmarkRunRequest(
	t *testing.T,
	mode string,
) adaptiveBenchmarkRunRequest {
	t.Helper()
	return adaptiveBenchmarkRunRequest{
		OwnerID:                   uuid.Must(uuid.NewV7()),
		OutputDir:                 filepath.Join(t.TempDir(), "report"),
		Mode:                      auraeval.AdaptiveBenchmarkMode(mode),
		ConfirmedSettingsOverride: mode == "qwen-portability",
	}
}

func adaptiveBenchmarkRunArgs(request adaptiveBenchmarkRunRequest) []string {
	args := []string{
		"--mode", string(request.Mode),
		"--owner-id", request.OwnerID.String(),
		"--output-dir", request.OutputDir,
	}
	if request.ConfirmedSettingsOverride {
		args = append(args, "--confirm-temporary-settings-override")
	}
	return args
}

type adaptiveBenchmarkRunExecutionFake struct {
	payload       []byte
	runErr        error
	closeErr      error
	finalizeErr   error
	runCalls      int
	closeCalls    int
	finalizeCalls int
	events        []string
}

type adaptiveBenchmarkShortWriter struct{}

func (adaptiveBenchmarkShortWriter) Write(payload []byte) (int, error) {
	return len(payload) - 1, nil
}

func (fake *adaptiveBenchmarkRunExecutionFake) Run(context.Context) error {
	fake.runCalls++
	fake.events = append(fake.events, "run")
	return fake.runErr
}

func (fake *adaptiveBenchmarkRunExecutionFake) Close() error {
	fake.closeCalls++
	fake.events = append(fake.events, "close")
	return fake.closeErr
}

func (fake *adaptiveBenchmarkRunExecutionFake) Finalize(
	context.Context,
) ([]byte, error) {
	fake.finalizeCalls++
	fake.events = append(fake.events, "finalize")
	return append([]byte(nil), fake.payload...), fake.finalizeErr
}
