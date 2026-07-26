package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"

	auraeval "github.com/chetto1983/aura/internal/eval"
)

const (
	adaptiveBenchmarkQwenModelsURL  = "http://127.0.0.1:18081/v1/models"
	adaptiveBenchmarkQwenParameters = int64(1_881_825_088)
	maxAdaptiveBenchmarkModelsBytes = 1 << 20
)

var (
	adaptiveBenchmarkQwenPathPattern = regexp.MustCompile(
		`(?:^|/)models--unsloth--Qwen3\.5-2B-GGUF/snapshots/` +
			`([0-9a-f]{40})/Qwen3\.5-2B-Q4_K_M\.gguf$`,
	)
	adaptiveBenchmarkServerVersionPattern = regexp.MustCompile(`^b[0-9]+$`)
	adaptiveBenchmarkRevisionPattern      = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	adaptiveBenchmarkSHA256Pattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type adaptiveBenchmarkHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type adaptiveBenchmarkQwenHostProvenance struct {
	ArtifactSHA256 string
	ServerVersion  string
	ServerRevision string
	ImageDigest    string
}

type adaptiveBenchmarkQwenInspector interface {
	Inspect(
		context.Context,
		string,
	) (adaptiveBenchmarkQwenHostProvenance, error)
}

type adaptiveBenchmarkModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
		Meta    struct {
			Parameters int64 `json:"n_params"`
		} `json:"meta"`
	} `json:"data"`
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

func preflightAdaptiveBenchmarkQwen(
	ctx context.Context,
	doer adaptiveBenchmarkHTTPDoer,
	inspector adaptiveBenchmarkQwenInspector,
) (auraeval.AdaptiveBenchmarkModel, error) {
	if doer == nil || inspector == nil {
		return auraeval.AdaptiveBenchmarkModel{}, errors.New(
			"adaptive benchmark Qwen preflight is unavailable",
		)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		adaptiveBenchmarkQwenModelsURL,
		nil,
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, err
	}
	response, err := doer.Do(request)
	if err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, fmt.Errorf(
			"query Qwen model metadata: %w",
			err,
		)
	}
	if response == nil || response.Body == nil {
		return auraeval.AdaptiveBenchmarkModel{}, errors.New(
			"qwen model metadata response is empty",
		)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return auraeval.AdaptiveBenchmarkModel{}, fmt.Errorf(
			"qwen model metadata returned status %d",
			response.StatusCode,
		)
	}
	payload, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			maxAdaptiveBenchmarkModelsBytes+1,
		),
	)
	if err != nil || len(payload) == 0 ||
		len(payload) > maxAdaptiveBenchmarkModelsBytes {
		return auraeval.AdaptiveBenchmarkModel{}, errors.New(
			"qwen model metadata size is invalid",
		)
	}
	var models adaptiveBenchmarkModelsResponse
	if err := json.Unmarshal(payload, &models); err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, fmt.Errorf(
			"decode Qwen model metadata: %w",
			err,
		)
	}
	modelPath, snapshot, err := validateAdaptiveBenchmarkQwenModels(models)
	if err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, err
	}
	host, err := inspector.Inspect(ctx, modelPath)
	if err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, fmt.Errorf(
			"inspect Qwen host artifact: %w",
			err,
		)
	}
	if err := validateAdaptiveBenchmarkQwenHost(host); err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, err
	}
	serverName := "llama.cpp"
	artifactName := auraeval.AdaptiveBenchmarkQwenArtifactName
	return auraeval.AdaptiveBenchmarkModel{
		ProviderID:       "llamacpp",
		ModelID:          "qwen",
		BaseURLClass:     auraeval.AdaptiveBenchmarkBaseURLLoopback,
		ServerName:       &serverName,
		ServerVersion:    stringPointer(host.ServerVersion),
		ServerRevision:   stringPointer(host.ServerRevision),
		ArtifactName:     &artifactName,
		ArtifactSHA256:   stringPointer(host.ArtifactSHA256),
		SnapshotRevision: stringPointer(snapshot),
		ImageDigest:      stringPointer(host.ImageDigest),
	}, nil
}

func validateAdaptiveBenchmarkQwenModels(
	models adaptiveBenchmarkModelsResponse,
) (string, string, error) {
	if models.Object != "list" ||
		len(models.Data) != 1 ||
		len(models.Models) != 1 {
		return "", "", errors.New(
			"qwen model metadata cardinality is invalid",
		)
	}
	data := models.Data[0]
	alias := models.Models[0]
	if data.Object != "model" ||
		data.OwnedBy != "llamacpp" ||
		data.Meta.Parameters != adaptiveBenchmarkQwenParameters ||
		data.ID == "" ||
		data.ID != alias.Name ||
		data.ID != alias.Model ||
		path.Base(data.ID) != auraeval.AdaptiveBenchmarkQwenArtifactName {
		return "", "", errors.New(
			"qwen model identity drifted",
		)
	}
	match := adaptiveBenchmarkQwenPathPattern.FindStringSubmatch(data.ID)
	if len(match) != 2 {
		return "", "", errors.New(
			"qwen model repository or snapshot is invalid",
		)
	}
	return data.ID, match[1], nil
}

func validateAdaptiveBenchmarkQwenHost(
	host adaptiveBenchmarkQwenHostProvenance,
) error {
	if host.ArtifactSHA256 !=
		auraeval.AdaptiveBenchmarkQwenArtifactSHA256 ||
		!adaptiveBenchmarkServerVersionPattern.MatchString(
			host.ServerVersion,
		) ||
		!adaptiveBenchmarkRevisionPattern.MatchString(
			host.ServerRevision,
		) ||
		!strings.HasPrefix(host.ImageDigest, "sha256:") ||
		!adaptiveBenchmarkSHA256Pattern.MatchString(
			strings.TrimPrefix(host.ImageDigest, "sha256:"),
		) {
		return errors.New("qwen host artifact provenance drifted")
	}
	return nil
}

func stringPointer(value string) *string {
	return &value
}
