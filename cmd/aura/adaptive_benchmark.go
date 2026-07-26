package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/google/uuid"
)

const (
	adaptiveEvalUsage              = "usage: aura eval adaptive {verify|seal-admission|benchmark}"
	maxAdaptiveBenchmarkInputBytes = 8 << 20
	adaptiveAdmissionManifestID    = "aura.adaptive.admission-manifest/v1"
)

type adaptiveBenchmarkCLI interface {
	VerifyCohort(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (auraeval.AdaptiveBenchmarkVerification, error)
	SealAdmission(
		context.Context,
		auraeval.AdaptiveBenchmarkAdmissionRequest,
	) (adaptive.CanaryAdmissionEvidence, error)
}

type adaptiveBenchmarkFactory func(
	context.Context,
) (adaptiveBenchmarkCLI, func(), error)

type adaptiveBenchmarkArtifactFile struct {
	Ref  adaptive.ChildArtifactRef `json:"ref"`
	Path string                    `json:"path"`
}

type adaptiveBenchmarkAdmissionManifest struct {
	SchemaID  string                          `json:"schema_id"`
	Revision  int                             `json:"revision"`
	OwnerID   string                          `json:"owner_id"`
	CohortID  string                          `json:"cohort_id"`
	Artifacts []adaptiveBenchmarkArtifactFile `json:"artifacts"`
}

func runEval(args []string) {
	if err := runEvalCommand(
		cliInvocationContext,
		args,
		os.Stdout,
		newAdaptiveBenchmarkCLI,
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runEvalCommand(
	ctx context.Context,
	args []string,
	output io.Writer,
	factory adaptiveBenchmarkFactory,
) error {
	return runEvalCommandWithBenchmarkFactory(
		ctx,
		args,
		output,
		factory,
		newAdaptiveBenchmarkRunExecution,
	)
}

func runEvalCommandWithBenchmarkFactory(
	ctx context.Context,
	args []string,
	output io.Writer,
	factory adaptiveBenchmarkFactory,
	runFactory adaptiveBenchmarkRunFactory,
) error {
	if len(args) < 2 || args[0] != "adaptive" {
		return errors.New(adaptiveEvalUsage)
	}
	switch args[1] {
	case "verify":
		return runAdaptiveBenchmarkVerify(
			ctx, args[2:], output, factory,
		)
	case "seal-admission":
		return runAdaptiveBenchmarkSealAdmission(
			ctx, args[2:], output, factory,
		)
	case "benchmark":
		return runAdaptiveBenchmarkRunCommand(
			ctx, args[2:], output, runFactory,
		)
	default:
		return errors.New(adaptiveEvalUsage)
	}
}

func runAdaptiveBenchmarkVerify(
	ctx context.Context,
	args []string,
	output io.Writer,
	factory adaptiveBenchmarkFactory,
) error {
	flags := flag.NewFlagSet("eval adaptive verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ownerText := flags.String("owner-id", "", "cohort owner UUID")
	cohortText := flags.String("cohort-id", "", "cohort UUID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("eval adaptive verify accepts flags only")
	}
	ownerID, err := parseAdaptiveBenchmarkUUID("owner-id", *ownerText)
	if err != nil {
		return err
	}
	cohortID, err := parseAdaptiveBenchmarkUUID("cohort-id", *cohortText)
	if err != nil {
		return err
	}
	workflow, closeWorkflow, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeWorkflow()
	report, err := workflow.VerifyCohort(ctx, ownerID, cohortID)
	if err != nil {
		return err
	}
	return writeJSON(output, report)
}

func runAdaptiveBenchmarkSealAdmission(
	ctx context.Context,
	args []string,
	output io.Writer,
	factory adaptiveBenchmarkFactory,
) error {
	request, err := loadAdaptiveBenchmarkAdmission(args)
	if err != nil {
		return err
	}
	fingerprint, err := auraeval.AdaptiveBenchmarkAdmissionFingerprint(request)
	if err != nil {
		return err
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok || operation.Fingerprint != fingerprint {
		return auraeval.ErrAdaptiveBenchmarkAdmissionInput
	}
	workflow, closeWorkflow, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeWorkflow()
	evidence, err := workflow.SealAdmission(ctx, request)
	if err != nil {
		return err
	}
	return writeJSON(output, evidence)
}

func loadAdaptiveBenchmarkAdmission(
	args []string,
) (auraeval.AdaptiveBenchmarkAdmissionRequest, error) {
	inputPath, confirmed, err := parseAdaptiveBenchmarkSealArgs(args)
	if err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	if !confirmed {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, errors.New(
			"eval adaptive seal-admission requires --confirm-operator-approval",
		)
	}
	payload, err := readAdaptiveBenchmarkFile(inputPath)
	if err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	var manifest adaptiveBenchmarkAdmissionManifest
	if err := decodeAdaptiveBenchmarkJSON(payload, &manifest); err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	manifestDigest := sha256.Sum256(payload)
	request, err := adaptiveBenchmarkAdmissionRequest(
		manifest,
		fmt.Sprintf("%x", manifestDigest),
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	if _, err := auraeval.AdaptiveBenchmarkAdmissionFingerprint(request); err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	return request, nil
}

func parseAdaptiveBenchmarkSealArgs(
	args []string,
) (string, bool, error) {
	if adaptiveBenchmarkFlagCount(args, "--input") != 1 {
		return "", false, errors.New(
			"eval adaptive seal-admission requires one --input",
		)
	}
	if adaptiveBenchmarkFlagCount(
		args,
		"--confirm-operator-approval",
	) > 1 {
		return "", false, errors.New(
			"eval adaptive seal-admission accepts one confirmation",
		)
	}
	flags := flag.NewFlagSet(
		"eval adaptive seal-admission",
		flag.ContinueOnError,
	)
	flags.SetOutput(io.Discard)
	inputPath := flags.String("input", "", "strict admission JSON manifest")
	confirmed := flags.Bool(
		"confirm-operator-approval",
		false,
		"confirm server-built operator approval for this cohort",
	)
	if err := flags.Parse(args); err != nil {
		return "", false, err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*inputPath) == "" {
		return "", false, errors.New(
			"eval adaptive seal-admission accepts flags only",
		)
	}
	return *inputPath, *confirmed, nil
}

func adaptiveBenchmarkFlagCount(args []string, name string) int {
	count := 0
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			count++
		}
	}
	return count
}

func adaptiveBenchmarkAdmissionRequest(
	manifest adaptiveBenchmarkAdmissionManifest,
	manifestSHA256 string,
) (auraeval.AdaptiveBenchmarkAdmissionRequest, error) {
	if manifest.SchemaID != adaptiveAdmissionManifestID ||
		manifest.Revision != 1 {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, errors.New(
			"adaptive admission manifest schema is invalid",
		)
	}
	ownerID, err := parseAdaptiveBenchmarkUUID("owner_id", manifest.OwnerID)
	if err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	cohortID, err := parseAdaptiveBenchmarkUUID("cohort_id", manifest.CohortID)
	if err != nil {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, err
	}
	if len(manifest.Artifacts) != 5 {
		return auraeval.AdaptiveBenchmarkAdmissionRequest{}, errors.New(
			"adaptive admission requires all five cohort artifacts",
		)
	}
	registrations := make(
		[]adaptive.EvidenceArtifactRegistration,
		0,
		len(manifest.Artifacts),
	)
	requiredKinds := []string{
		"interference_plan", "look_plan", "quality_outcome_model",
		"harm_outcome_model", "ope_plan",
	}
	for index, artifact := range manifest.Artifacts {
		if artifact.Ref.Kind != requiredKinds[index] ||
			strings.TrimSpace(artifact.Path) == "" {
			return auraeval.AdaptiveBenchmarkAdmissionRequest{}, errors.New(
				"adaptive admission artifact order or path is invalid",
			)
		}
		canonical, err := readAdaptiveBenchmarkFile(artifact.Path)
		if err != nil {
			return auraeval.AdaptiveBenchmarkAdmissionRequest{}, fmt.Errorf(
				"read adaptive %s artifact: %w",
				artifact.Ref.Kind,
				err,
			)
		}
		registrations = append(
			registrations,
			adaptive.EvidenceArtifactRegistration{
				Ref: artifact.Ref, Canonical: canonical,
			},
		)
	}
	return auraeval.AdaptiveBenchmarkAdmissionRequest{
		Command:        auraeval.AdaptiveBenchmarkAdmissionCommand,
		Confirmed:      true,
		ManifestSHA256: manifestSHA256,
		OwnerID:        ownerID,
		CohortID:       cohortID,
		Artifacts:      registrations,
	}, nil
}

func newAdaptiveBenchmarkCLI(
	ctx context.Context,
) (adaptiveBenchmarkCLI, func(), error) {
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return nil, nil, err
	}
	ledger := adaptive.NewStore(pool, adaptive.StoreConfig{})
	workflow, err := auraeval.NewAdaptiveBenchmark(
		adaptive.NewCohortStore(pool, ledger),
		adaptive.NewEvidenceStore(pool),
	)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return workflow, pool.Close, nil
}

func decodeAdaptiveBenchmarkJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode adaptive benchmark input: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("adaptive benchmark input has trailing data")
	}
	canonical, err := json.Marshal(target)
	if err != nil || !bytes.Equal(canonical, payload) {
		return errors.New("adaptive benchmark input is noncanonical")
	}
	return nil
}

func readAdaptiveBenchmarkFile(path string) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // Local operator selects a bounded artifact file.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	payload, err := io.ReadAll(
		io.LimitReader(file, maxAdaptiveBenchmarkInputBytes+1),
	)
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxAdaptiveBenchmarkInputBytes {
		return nil, errors.New("adaptive benchmark input size is invalid")
	}
	return payload, nil
}

func parseAdaptiveBenchmarkUUID(label string, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, fmt.Errorf(
			"adaptive benchmark %s must be a canonical UUID",
			label,
		)
	}
	return id, nil
}
