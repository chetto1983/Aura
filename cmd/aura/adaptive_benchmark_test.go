package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestRunEvalCommandVerifyUsesAdaptiveWorkflow(t *testing.T) {
	t.Parallel()
	ownerID := uuid.Must(uuid.NewV7())
	cohortID := uuid.Must(uuid.NewV7())
	fake := &adaptiveBenchmarkCLIFake{
		verification: auraeval.AdaptiveBenchmarkVerification{
			CohortID: cohortID, CohortSHA256: strings.Repeat("a", 64),
			ClosedLedgerSHA256: strings.Repeat("b", 64),
		},
	}
	var output bytes.Buffer

	err := runEvalCommand(
		t.Context(),
		[]string{
			"adaptive", "verify",
			"--owner-id", ownerID.String(),
			"--cohort-id", cohortID.String(),
		},
		&output,
		func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
			return fake, func() {}, nil
		},
	)
	if err != nil {
		t.Fatalf("runEvalCommand: %v", err)
	}
	if fake.verifyOwnerID != ownerID || fake.verifyCohortID != cohortID {
		t.Fatalf(
			"verify scope = %s/%s, want %s/%s",
			fake.verifyOwnerID,
			fake.verifyCohortID,
			ownerID,
			cohortID,
		)
	}
	if !strings.Contains(output.String(), fake.verification.ClosedLedgerSHA256) {
		t.Fatalf("verify output = %s", output.String())
	}
}

func TestRunEvalCommandRejectsRemovedRecordOutcome(t *testing.T) {
	t.Parallel()
	inputPath := writeAdaptiveBenchmarkJSON(
		t,
		"removed-outcome.json",
		map[string]any{"owner_id": uuid.Must(uuid.NewV7())},
	)
	factoryCalls := 0

	err := runEvalCommand(
		t.Context(),
		[]string{"adaptive", "record-outcome", "--input", inputPath},
		&bytes.Buffer{},
		func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
			factoryCalls++
			return &adaptiveBenchmarkCLIFake{}, func() {}, nil
		},
	)
	if err == nil || factoryCalls != 0 {
		t.Fatalf(
			"removed record-outcome error=%v factory_calls=%d, want usage failure",
			err,
			factoryCalls,
		)
	}
}

func TestRunEvalCommandSealAdmissionRejectsIncompleteInputBeforeFactory(
	t *testing.T,
) {
	t.Parallel()
	manifest := adaptiveBenchmarkAdmissionManifest{
		SchemaID: "aura.adaptive.admission-manifest/v1",
		Revision: 1,
		OwnerID:  uuid.Must(uuid.NewV7()).String(),
		CohortID: uuid.Must(uuid.NewV7()).String(),
		Artifacts: []adaptiveBenchmarkArtifactFile{
			{Ref: adaptive.ChildArtifactRef{Kind: "interference_plan"}, Path: "one.json"},
		},
	}
	inputPath := writeAdaptiveBenchmarkJSON(t, "admission.json", manifest)
	factoryCalls := 0

	err := runEvalCommand(
		t.Context(),
		[]string{
			"adaptive", "seal-admission", "--input", inputPath,
			"--confirm-operator-approval",
		},
		&bytes.Buffer{},
		func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
			factoryCalls++
			return &adaptiveBenchmarkCLIFake{}, func() {}, nil
		},
	)
	if err == nil || factoryCalls != 0 {
		t.Fatalf(
			"incomplete admission error=%v factory_calls=%d, want pre-factory failure",
			err,
			factoryCalls,
		)
	}
}

func TestRunEvalCommandSealAdmissionRequiresExplicitConfirmation(t *testing.T) {
	t.Parallel()
	manifestPath := writeAdaptiveAdmissionManifest(t)
	factoryCalls := 0

	err := runEvalCommand(
		t.Context(),
		[]string{"adaptive", "seal-admission", "--input", manifestPath},
		&bytes.Buffer{},
		func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
			factoryCalls++
			return &adaptiveBenchmarkCLIFake{}, func() {}, nil
		},
	)
	if err == nil || factoryCalls != 0 {
		t.Fatalf(
			"unconfirmed admission error=%v factory_calls=%d, want pre-factory failure",
			err,
			factoryCalls,
		)
	}
}

func TestRunEvalCommandSealAdmissionRejectsNoncanonicalManifestBeforeFactory(
	t *testing.T,
) {
	t.Parallel()
	validPath := writeAdaptiveAdmissionManifest(t)
	valid, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	reordered := bytes.Replace(
		valid,
		[]byte(`{"schema_id":"aura.adaptive.admission-manifest/v1","revision":1`),
		[]byte(`{"revision":1,"schema_id":"aura.adaptive.admission-manifest/v1"`),
		1,
	)
	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name: "missing schema",
			payload: bytes.Replace(
				valid,
				[]byte(`"schema_id":"aura.adaptive.admission-manifest/v1",`),
				nil,
				1,
			),
		},
		{name: "field order", payload: reordered},
		{name: "trailing data", payload: append(append([]byte(nil), valid...), []byte(`{}`)...)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(path, testCase.payload, 0o600); err != nil {
				t.Fatal(err)
			}
			factoryCalls := 0
			err := runEvalCommand(
				t.Context(),
				[]string{
					"adaptive", "seal-admission", "--input", path,
					"--confirm-operator-approval",
				},
				io.Discard,
				func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
					factoryCalls++
					return &adaptiveBenchmarkCLIFake{}, func() {}, nil
				},
			)
			if err == nil || factoryCalls != 0 {
				t.Fatalf(
					"noncanonical manifest error=%v factory_calls=%d",
					err,
					factoryCalls,
				)
			}
		})
	}
}

func TestAdaptiveBenchmarkInputSeamsRejectTrailingData(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "trailing.json")
	if err := os.WriteFile(path, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := readAdaptiveBenchmarkFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]any
	if err := decodeAdaptiveBenchmarkJSON(payload, &target); err == nil {
		t.Fatal("adaptive benchmark decoder accepted trailing JSON")
	}
}

func TestAdaptiveBenchmarkInputSeamsRejectUnknownDuplicateAndNoncanonicalJSON(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
	}{
		{name: "unknown field", payload: `{"owner_id":"first","unknown":true}`},
		{name: "duplicate", payload: `{"owner_id":"first","owner_id":"second"}`},
		{name: "noncanonical whitespace", payload: ` {"owner_id":"first"}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, []byte(testCase.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := readAdaptiveBenchmarkFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var target struct {
				OwnerID string `json:"owner_id"`
			}
			if err := decodeAdaptiveBenchmarkJSON(payload, &target); err == nil {
				t.Fatalf(
					"adaptive benchmark decoder accepted %s input",
					testCase.name,
				)
			}
		})
	}
}

func TestReadAdaptiveBenchmarkFileRejectsOversizeInput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "oversize.json")
	payload := bytes.Repeat([]byte("x"), maxAdaptiveBenchmarkInputBytes+1)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAdaptiveBenchmarkFile(path); err == nil {
		t.Fatal("adaptive benchmark file loader accepted oversized input")
	}
}

func TestRunEvalCommandRejectsInvalidArgumentsBeforeFactory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing adaptive command"},
		{name: "wrong command", args: []string{"other", "verify"}},
		{name: "unknown subcommand", args: []string{"adaptive", "unknown"}},
		{name: "verify flag", args: []string{"adaptive", "verify", "--unknown"}},
		{name: "verify trailing argument", args: []string{"adaptive", "verify", "trailing"}},
		{name: "verify owner UUID", args: []string{"adaptive", "verify", "--owner-id", "not-a-uuid"}},
		{name: "verify cohort UUID", args: []string{
			"adaptive", "verify", "--owner-id", uuid.Must(uuid.NewV7()).String(),
			"--cohort-id", uuid.Nil.String(),
		}},
		{name: "seal flag", args: []string{"adaptive", "seal-admission", "--unknown"}},
		{name: "seal input required", args: []string{"adaptive", "seal-admission"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			factoryCalls := 0
			err := runEvalCommand(
				t.Context(), testCase.args, &bytes.Buffer{},
				func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
					factoryCalls++
					return &adaptiveBenchmarkCLIFake{}, func() {}, nil
				},
			)
			if err == nil || factoryCalls != 0 {
				t.Fatalf("runEvalCommand() error=%v factory_calls=%d", err, factoryCalls)
			}
		})
	}
}

func TestAdaptiveBenchmarkCommandsPropagateFactoryAndWorkflowErrors(t *testing.T) {
	t.Parallel()
	ownerID := uuid.Must(uuid.NewV7())
	cohortID := uuid.Must(uuid.NewV7())
	admissionPath := writeAdaptiveAdmissionManifest(t)
	factoryErr := errors.New("factory unavailable")
	workflowErr := errors.New("workflow unavailable")
	tests := []struct {
		name       string
		args       []string
		fake       *adaptiveBenchmarkCLIFake
		factoryErr error
		want       error
		wantClosed int
	}{
		{
			name: "verify factory", args: []string{
				"adaptive", "verify", "--owner-id", ownerID.String(),
				"--cohort-id", cohortID.String(),
			},
			factoryErr: factoryErr, want: factoryErr,
		},
		{
			name: "verify workflow closes", args: []string{
				"adaptive", "verify", "--owner-id", ownerID.String(),
				"--cohort-id", cohortID.String(),
			},
			fake: &adaptiveBenchmarkCLIFake{verifyErr: workflowErr},
			want: workflowErr, wantClosed: 1,
		},
		{
			name: "seal factory", args: []string{
				"adaptive", "seal-admission", "--input", admissionPath,
				"--confirm-operator-approval",
			},
			factoryErr: factoryErr, want: factoryErr,
		},
		{
			name: "seal workflow", args: []string{
				"adaptive", "seal-admission", "--input", admissionPath,
				"--confirm-operator-approval",
			},
			fake: &adaptiveBenchmarkCLIFake{sealErr: workflowErr},
			want: workflowErr, wantClosed: 1,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			closed := 0
			runContext := t.Context()
			runArgs := testCase.args
			if len(runArgs) >= 2 &&
				runArgs[0] == "adaptive" &&
				runArgs[1] == "seal-admission" {
				runContext, runArgs = prepareAdaptiveAdmissionCommand(
					t,
					runArgs,
				)
			}
			err := runEvalCommand(
				runContext, runArgs, &bytes.Buffer{},
				func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
					return testCase.fake, func() { closed++ }, testCase.factoryErr
				},
			)
			if !errors.Is(err, testCase.want) || closed != testCase.wantClosed {
				t.Fatalf(
					"runEvalCommand() error=%v closed=%d, want %v/%d",
					err, closed, testCase.want, testCase.wantClosed,
				)
			}
		})
	}
}

func TestRunEvalCommandSealAdmissionSucceedsAndCloses(t *testing.T) {
	t.Parallel()
	manifestPath := writeAdaptiveAdmissionManifest(t)
	fake := &adaptiveBenchmarkCLIFake{}
	closed := 0
	var output bytes.Buffer
	runContext, runArgs := prepareAdaptiveAdmissionCommand(
		t,
		[]string{
			"adaptive", "seal-admission", "--input", manifestPath,
			"--confirm-operator-approval",
		},
	)

	err := runEvalCommand(
		runContext,
		runArgs,
		&output,
		func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
			return fake, func() { closed++ }, nil
		},
	)
	if err != nil {
		t.Fatalf("runEvalCommand: %v", err)
	}
	if closed != 1 || len(fake.admission.Artifacts) != 5 || output.Len() == 0 {
		t.Fatalf(
			"seal result closed=%d admission=%#v output=%q",
			closed, fake.admission, output.String(),
		)
	}
}

func TestAdaptiveBenchmarkAdmissionRequestRejectsInvalidManifest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*adaptiveBenchmarkAdmissionManifest)
	}{
		{name: "owner UUID", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.OwnerID = "invalid"
		}},
		{name: "cohort UUID", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.CohortID = "invalid"
		}},
		{name: "duplicate kind", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.Artifacts[1].Ref.Kind = manifest.Artifacts[0].Ref.Kind
		}},
		{name: "empty path", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.Artifacts[0].Path = " "
		}},
		{name: "unreadable artifact", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.Artifacts[0].Path = filepath.Join(t.TempDir(), "missing.json")
		}},
		{name: "missing required kind", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.Artifacts[0].Ref.Kind = "unexpected"
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			manifest := validAdaptiveAdmissionManifest(t)
			testCase.mutate(&manifest)
			if _, err := adaptiveBenchmarkAdmissionRequest(
				manifest,
				strings.Repeat("a", 64),
			); err == nil {
				t.Fatal("adaptiveBenchmarkAdmissionRequest accepted invalid manifest")
			}
		})
	}
}

func TestReadAdaptiveBenchmarkFileRejectsMissingAndEmptyInput(t *testing.T) {
	t.Parallel()
	emptyPath := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(t.TempDir(), "missing.json"),
		emptyPath,
	} {
		if _, err := readAdaptiveBenchmarkFile(path); err == nil {
			t.Fatalf("readAdaptiveBenchmarkFile(%q) succeeded", path)
		}
	}
}

type adaptiveBenchmarkCLIFake struct {
	verification   auraeval.AdaptiveBenchmarkVerification
	verifyOwnerID  uuid.UUID
	verifyCohortID uuid.UUID
	admission      auraeval.AdaptiveBenchmarkAdmissionRequest
	verifyErr      error
	sealErr        error
}

func (fake *adaptiveBenchmarkCLIFake) VerifyCohort(
	_ context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (auraeval.AdaptiveBenchmarkVerification, error) {
	fake.verifyOwnerID = ownerID
	fake.verifyCohortID = cohortID
	return fake.verification, fake.verifyErr
}

func (fake *adaptiveBenchmarkCLIFake) SealAdmission(
	_ context.Context,
	request auraeval.AdaptiveBenchmarkAdmissionRequest,
) (adaptive.CanaryAdmissionEvidence, error) {
	fake.admission = request
	return adaptive.CanaryAdmissionEvidence{}, fake.sealErr
}

func writeAdaptiveAdmissionManifest(t *testing.T) string {
	t.Helper()
	return writeAdaptiveBenchmarkJSON(
		t, "admission.json", validAdaptiveAdmissionManifest(t),
	)
}

func writeAdaptiveBenchmarkJSON(t *testing.T, name string, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func prepareAdaptiveAdmissionCommand(
	t *testing.T,
	args []string,
) (context.Context, []string) {
	t.Helper()
	fullArgs := append([]string{"eval"}, args...)
	fullArgs = append(
		fullArgs,
		"--operation-key",
		"adaptive-command-test-key",
	)
	ctx, cleaned, err := prepareCLIIdempotency(
		t.Context(),
		fullArgs,
		io.Discard,
	)
	if err != nil {
		t.Fatalf("prepare adaptive admission command: %v", err)
	}
	return ctx, cleaned[1:]
}
