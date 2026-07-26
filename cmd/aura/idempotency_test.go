package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/google/uuid"
)

type cliMemoryRegistry struct {
	request          *idempotency.BeginRequest
	replay           *idempotency.ReplayResult
	marked           int
	recover          bool
	recovered        int
	activeClaimToken idempotency.ClaimToken
}

func (m *cliMemoryRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	if m.request == nil {
		copyRequest := request
		m.request = &copyRequest
		m.activeClaimToken = 1
		return idempotency.BeginDecision{
			Decision: idempotency.DecisionAcquired, ClaimToken: m.activeClaimToken,
		}, nil
	}
	if m.request.Operation != request.Operation || m.request.Fingerprint != request.Fingerprint {
		return idempotency.BeginDecision{Decision: idempotency.DecisionConflict}, idempotency.ErrConflict
	}
	if m.replay == nil {
		return idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: time.Second}, nil
	}
	return idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: m.replay}, nil
}

func (m *cliMemoryRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	if m.request == nil || m.request.Operation != request.Operation ||
		m.request.Fingerprint != request.Fingerprint ||
		request.ClaimToken != m.activeClaimToken {
		return errors.New("unexpected CLI completion")
	}
	result := request.Result
	m.replay = &result
	return nil
}

func (m *cliMemoryRegistry) MarkIndeterminate(
	_ context.Context,
	_ idempotency.OperationKey,
	_ [32]byte,
	claimToken idempotency.ClaimToken,
) error {
	if claimToken != m.activeClaimToken {
		return errors.New("unexpected CLI indeterminate claim token")
	}
	m.marked++
	return nil
}

func (m *cliMemoryRegistry) RecoverExpired(
	_ context.Context,
	request idempotency.BeginRequest,
) (idempotency.ClaimToken, bool, error) {
	m.recovered++
	if m.request == nil ||
		m.request.Operation != request.Operation ||
		m.request.Fingerprint != request.Fingerprint {
		return 0, false, nil
	}
	if !m.recover {
		return 0, false, nil
	}
	if m.activeClaimToken == 0 {
		m.activeClaimToken = 1
	}
	m.activeClaimToken++
	return m.activeClaimToken, true, nil
}

func TestPrepareCLIIdempotencyExplicitKeyIsStable(t *testing.T) {
	t.Parallel()

	args := []string{"task", "run_now", "task-1", "--operation-key", "caller-key"}
	first, firstArgs, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	second, secondArgs, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	firstKey, firstFingerprint, firstOK := cliOperationFromContext(first)
	secondKey, secondFingerprint, secondOK := cliOperationFromContext(second)
	if !firstOK || !secondOK {
		t.Fatal("prepared CLI contexts lack operation metadata")
	}
	if firstKey != secondKey || firstFingerprint != secondFingerprint || firstKey != "caller-key" {
		t.Fatalf("retry operations differ: %q/%x != %q/%x", firstKey, firstFingerprint, secondKey, secondFingerprint)
	}
	if strings.Join(firstArgs, " ") != "task run_now task-1" || strings.Join(secondArgs, " ") != "task run_now task-1" {
		t.Fatalf("operation flag leaked to command args: %q / %q", firstArgs, secondArgs)
	}
}

func TestPrepareCLIIdempotencyUsesDurableServiceIdentity(t *testing.T) {
	t.Parallel()

	ctx, _, err := prepareCLIIdempotency(context.Background(), []string{
		"neo4j", "migrate", "--operation-key", "deploy-migration-key",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		t.Fatal("prepared CLI context lacks operation metadata")
	}
	const durableCLIIdentity = "00000000-0000-0000-0000-000000000039"
	if operation.Key.IdentityID != durableCLIIdentity {
		t.Fatalf("CLI operation identity = %q, want durable service identity %q", operation.Key.IdentityID, durableCLIIdentity)
	}
}

func TestPrepareCLIIdempotencyDisplaysGeneratedKeyBeforeExecution(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ctx, _, err := prepareCLIIdempotency(context.Background(), []string{"chat", "delete", "conv-1"}, &out)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	key, _, ok := cliOperationFromContext(ctx)
	if !ok || key == "" {
		t.Fatalf("generated operation missing: key=%q ok=%v", key, ok)
	}
	if !strings.Contains(out.String(), key) {
		t.Fatalf("generated key %q was not displayed in %q", key, out.String())
	}
}

func TestPrepareCLIIdempotencyRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	_, _, err := prepareCLIIdempotency(context.Background(), []string{"task", "cancel", "task-1", "--operation-key", "bad\nkey"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("invalid operation key accepted")
	}
}

func TestPrepareCLIIdempotencyFingerprintsNormalizedIntent(t *testing.T) {
	t.Parallel()

	first, _, err := prepareCLIIdempotency(context.Background(), []string{"task", "cancel", "task-1", "--operation-key", "same-key"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	changed, _, err := prepareCLIIdempotency(context.Background(), []string{"task", "cancel", "task-2", "--operation-key", "same-key"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	_, firstFingerprint, _ := cliOperationFromContext(first)
	_, changedFingerprint, _ := cliOperationFromContext(changed)
	if firstFingerprint == changedFingerprint {
		t.Fatal("changed command payload produced the same operation fingerprint")
	}
}

func TestPrepareCLIIdempotencyPreservesGenericFingerprintGolden(t *testing.T) {
	t.Parallel()
	ctx, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{
			"task", "cancel", "task-1",
			"--operation-key", "generic-golden-key",
		},
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, fingerprint, ok := cliOperationFromContext(ctx)
	if !ok {
		t.Fatal("generic command lacks operation fingerprint")
	}
	want := sha256.Sum256(
		[]byte(`{"command":"task cancel","args":["task-1"]}`),
	)
	if fingerprint != want {
		t.Fatalf("generic fingerprint = %x, want legacy golden %x", fingerprint, want)
	}
}

func TestCLIMutationPathUsesLongestExactAdaptiveCommand(t *testing.T) {
	t.Parallel()

	command, mutating := cliMutationPath([]string{
		"eval", "adaptive", "seal-admission", "--input", "admission.json",
	})
	if !mutating || command != "eval adaptive seal-admission" {
		t.Fatalf("seal mutation = %q/%t, want exact three-token command", command, mutating)
	}
	if command, mutating := cliMutationPath([]string{
		"eval", "adaptive", "verify",
	}); mutating || command != "" {
		t.Fatalf("verify mutation = %q/%t, want read-only", command, mutating)
	}
	if command, mutating := cliMutationPath([]string{
		"eval", "adaptive", "seal-admission-extra",
	}); mutating || command != "" {
		t.Fatalf("prefix mutation = %q/%t, want exact rejection", command, mutating)
	}
}

func TestPrepareCLIIdempotencyRejectsOperationKeyForAdaptiveVerify(
	t *testing.T,
) {
	t.Parallel()
	_, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{
			"eval", "adaptive", "verify",
			"--owner-id", "11111111-1111-4111-8111-111111111111",
			"--cohort-id", "22222222-2222-4222-8222-222222222222",
			"--operation-key", "read-only-key",
		},
		io.Discard,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "only valid for mutating commands") {
		t.Fatalf("adaptive verify operation key error = %v", err)
	}
}

func TestPrepareCLIIdempotencyBindsAdaptiveAdmissionSemanticIntent(
	t *testing.T,
) {
	t.Parallel()
	baseManifest := validAdaptiveAdmissionManifest(t)
	basePath := writeAdaptiveBenchmarkJSON(t, "admission.json", baseManifest)
	base := adaptiveAdmissionFingerprintForTest(t, basePath)
	retry := adaptiveAdmissionFingerprintForTest(t, basePath)
	if base != retry {
		t.Fatal("unchanged admission semantic intent changed fingerprint")
	}

	tests := []struct {
		name   string
		mutate func(*adaptiveBenchmarkAdmissionManifest)
	}{
		{name: "manifest path bytes", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			copied := filepath.Join(t.TempDir(), "copied-child.json")
			payload, err := os.ReadFile(manifest.Artifacts[0].Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(copied, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			manifest.Artifacts[0].Path = copied
		}},
		{name: "owner", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.OwnerID = "11111111-1111-4111-8111-111111111111"
		}},
		{name: "cohort", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.CohortID = "22222222-2222-4222-8222-222222222222"
		}},
		{name: "reference", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.Artifacts[0].Ref.ArtifactID =
				"33333333-3333-4333-8333-333333333333"
		}},
		{name: "child digest", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			payload, err := os.ReadFile(manifest.Artifacts[0].Path)
			if err != nil {
				t.Fatal(err)
			}
			payload = bytes.Replace(
				payload,
				[]byte(strings.Repeat("a", 64)),
				[]byte(strings.Repeat("9", 64)),
				1,
			)
			if err := os.WriteFile(
				manifest.Artifacts[0].Path,
				payload,
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			manifest.Artifacts[0].Ref.ArtifactSHA256 = sha256HexForTest(payload)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			manifest := validAdaptiveAdmissionManifest(t)
			testCase.mutate(&manifest)
			path := writeAdaptiveBenchmarkJSON(t, "changed.json", manifest)
			changed := adaptiveAdmissionFingerprintForTest(t, path)
			if changed == base {
				t.Fatal("changed adaptive admission retained semantic fingerprint")
			}
		})
	}
}

func TestPrepareCLIIdempotencyRejectsArtifactDigestMismatchBeforeBegin(
	t *testing.T,
) {
	t.Parallel()
	manifest := validAdaptiveAdmissionManifest(t)
	if err := os.WriteFile(
		manifest.Artifacts[0].Path,
		[]byte(`{"schema_id":"fixture/v1","revision":1,"changed":true}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	path := writeAdaptiveBenchmarkJSON(t, "mismatched.json", manifest)
	if _, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{
			"eval", "adaptive", "seal-admission",
			"--input", path,
			"--confirm-operator-approval",
			"--operation-key", "digest-mismatch-key",
		},
		io.Discard,
	); err == nil {
		t.Fatal("artifact bytes mismatching the manifest reference reached Begin")
	}
}

func TestPrepareCLIIdempotencyRejectsMissingTypedChildFieldsBeforeBegin(
	t *testing.T,
) {
	t.Parallel()
	manifest := validAdaptiveAdmissionManifest(t)
	missing := []byte(
		`{"schema_id":"aura.adaptive.interference-plan/v1","revision":1}`,
	)
	if err := os.WriteFile(
		manifest.Artifacts[0].Path,
		missing,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts[0].Ref.ArtifactSHA256 = sha256HexForTest(missing)
	path := writeAdaptiveBenchmarkJSON(t, "missing-typed-fields.json", manifest)
	if _, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{
			"eval", "adaptive", "seal-admission",
			"--input", path,
			"--confirm-operator-approval",
			"--operation-key", "typed-child-key",
		},
		io.Discard,
	); err == nil {
		t.Fatal("typed child missing registered fields reached Begin")
	}
}

func TestRunEvalCommandSealAdmissionRejectsMutationAfterPreparation(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*adaptiveBenchmarkAdmissionManifest)
	}{
		{name: "manifest", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			manifest.OwnerID = uuid.Must(uuid.NewV7()).String()
		}},
		{name: "artifact", mutate: func(manifest *adaptiveBenchmarkAdmissionManifest) {
			if err := os.WriteFile(
				manifest.Artifacts[0].Path,
				[]byte(`{"schema_id":"fixture/v1","revision":1,"changed":true}`),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			manifest := validAdaptiveAdmissionManifest(t)
			path := writeAdaptiveBenchmarkJSON(t, "admission.json", manifest)
			prepared, cleaned, err := prepareCLIIdempotency(
				t.Context(),
				[]string{
					"eval", "adaptive", "seal-admission", "--input", path,
					"--confirm-operator-approval",
					"--operation-key", "stable-admission-key",
				},
				io.Discard,
			)
			if err != nil {
				t.Fatalf("prepare parent admission: %v", err)
			}
			testCase.mutate(&manifest)
			if testCase.name == "manifest" {
				payload, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, payload, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			factoryCalls := 0
			err = runEvalCommand(
				prepared,
				cleaned[1:],
				io.Discard,
				func(context.Context) (adaptiveBenchmarkCLI, func(), error) {
					factoryCalls++
					return &adaptiveBenchmarkCLIFake{}, func() {}, nil
				},
			)
			if err == nil || factoryCalls != 0 {
				t.Fatalf(
					"mutation error=%v factory_calls=%d, want pre-factory conflict",
					err,
					factoryCalls,
				)
			}
		})
	}
}

func adaptiveAdmissionFingerprintForTest(
	t *testing.T,
	path string,
) [32]byte {
	t.Helper()
	ctx, _, err := prepareCLIIdempotency(
		context.Background(),
		[]string{
			"eval", "adaptive", "seal-admission",
			"--input", path,
			"--confirm-operator-approval",
			"--operation-key", "stable-admission-key",
		},
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, fingerprint, ok := cliOperationFromContext(ctx)
	if !ok {
		t.Fatal("adaptive admission lacks operation fingerprint")
	}
	return fingerprint
}

func TestRunCLIIdempotentExecutesOnceAndReplaysRealOutput(t *testing.T) {
	t.Parallel()

	args := []string{"task", "cancel", "task-1", "--operation-key", "stable-cli-key"}
	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), args, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	registry := &cliMemoryRegistry{}
	effects := 0
	executor := func(ctx context.Context, childArgs []string, stdout, _ io.Writer) int {
		effects++
		operation, ok := idempotency.OperationFromContext(ctx)
		if !ok || operation.ClaimToken != 1 {
			t.Fatalf("executor operation = %+v/%t, want claim token 1", operation, ok)
		}
		if !slices.Contains(childArgs, "stable-cli-key") {
			t.Errorf("child args = %q, operation key missing", childArgs)
		}
		_, _ = io.WriteString(stdout, "cancelled task-1\n")
		return 0
	}
	var firstOut, firstErr bytes.Buffer
	if code := runCLIIdempotent(
		ctx, cleaned, &firstOut, &firstErr, registry, executor, false,
	); code != 0 {
		t.Fatalf("first exit = %d, stderr=%q", code, firstErr.String())
	}
	var replayOut, replayErr bytes.Buffer
	if code := runCLIIdempotent(
		ctx, cleaned, &replayOut, &replayErr, registry, executor, false,
	); code != 0 {
		t.Fatalf("replay exit = %d, stderr=%q", code, replayErr.String())
	}
	if effects != 1 {
		t.Fatalf("effects = %d, want one", effects)
	}
	if firstOut.String() != replayOut.String() || replayOut.String() != "cancelled task-1\n" {
		t.Fatalf("outputs first=%q replay=%q", firstOut.String(), replayOut.String())
	}
}

func TestRunCLIIdempotentFailureBecomesTerminalIndeterminate(t *testing.T) {
	t.Parallel()

	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), []string{"config", "set", "x", "y", "--operation-key", "failed-cli-key"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	registry := &cliMemoryRegistry{}
	executor := func(context.Context, []string, io.Writer, io.Writer) int { return 7 }
	if code := runCLIIdempotent(
		ctx, cleaned, io.Discard, io.Discard, registry, executor, false,
	); code != 7 {
		t.Fatalf("exit = %d, want 7", code)
	}
	if registry.marked != 1 || registry.replay != nil {
		t.Fatalf("marked=%d replay=%v, want terminal indeterminate", registry.marked, registry.replay)
	}
}

func TestExecuteCLIIdempotentParentMigrationUsesSchemaOwner(t *testing.T) {
	ctx, cleaned, err := prepareCLIIdempotency(context.Background(), []string{
		"db", "migrate", "--operation-key", "migration-owner-key",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	original := cliMutationCommands["db migrate"]
	t.Cleanup(func() { cliMutationCommands["db migrate"] = original })
	called := 0
	meta := original
	meta.Execute = func(_ context.Context, args []string, _, _ io.Writer) int {
		called++
		if !slices.Contains(args, "migration-owner-key") {
			t.Fatalf("migration child args = %q, stable operation key missing", args)
		}
		return 0
	}
	cliMutationCommands["db migrate"] = meta

	handled, code := executeCLIIdempotentParent(ctx, cleaned, io.Discard, io.Discard)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d, want schema owner success", handled, code)
	}
	if called != 1 {
		t.Fatalf("migration executions=%d, want one", called)
	}
}
