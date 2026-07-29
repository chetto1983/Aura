package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	auraeval "github.com/chetto1983/aura/internal/eval"
)

const qwenModelPathFixture = "/root/.cache/huggingface/hub/" +
	"models--unsloth--Qwen3.5-2B-GGUF/snapshots/" +
	"f6d5376be1edb4d416d56da11e5397a961aca8ae/" +
	"Qwen3.5-2B-Q4_K_M.gguf"

func TestPreflightAdaptiveBenchmarkQwenVerifiesServerAndArtifact(t *testing.T) {
	t.Parallel()
	doer := adaptiveBenchmarkHTTPDoerFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		if request.Method != http.MethodGet ||
			request.URL.String() !=
				"http://127.0.0.1:18081/v1/models" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		return qwenModelsResponse(
			`{"object":"list","data":[{"id":"` +
				qwenModelPathFixture +
				`","object":"model","owned_by":"llamacpp","meta":{` +
				`"n_params":1881825088}}],` +
				`"models":[{"name":"` +
				qwenModelPathFixture +
				`","model":"` +
				qwenModelPathFixture +
				`"}]}`,
		), nil
	})
	inspector := &adaptiveBenchmarkQwenInspectorFake{
		provenance: adaptiveBenchmarkQwenHostProvenance{
			ArtifactSHA256: auraeval.AdaptiveBenchmarkQwenArtifactSHA256,
			ServerVersion:  "b9994",
			ServerRevision: "14d3ba45f",
			ImageDigest: "sha256:" +
				"b57dce073940d6f347d59230d4d28fc947db8948f2eb162326008380b07bab77",
		},
	}

	model, err := preflightAdaptiveBenchmarkQwen(
		t.Context(),
		doer,
		inspector,
	)
	if err != nil {
		t.Fatalf("preflightAdaptiveBenchmarkQwen: %v", err)
	}
	if inspector.path != qwenModelPathFixture ||
		model.ProviderID != "llamacpp" ||
		model.ModelID != "qwen" ||
		model.BaseURLClass != auraeval.AdaptiveBenchmarkBaseURLLoopback ||
		model.ArtifactName == nil ||
		*model.ArtifactName != auraeval.AdaptiveBenchmarkQwenArtifactName ||
		model.ArtifactSHA256 == nil ||
		*model.ArtifactSHA256 !=
			auraeval.AdaptiveBenchmarkQwenArtifactSHA256 ||
		model.SnapshotRevision == nil ||
		*model.SnapshotRevision !=
			"f6d5376be1edb4d416d56da11e5397a961aca8ae" {
		t.Fatalf("model = %#v inspector_path=%q", model, inspector.path)
	}
}

func TestPreflightAdaptiveBenchmarkQwenRejectsEveryDrift(t *testing.T) {
	t.Parallel()
	valid := `{"object":"list","data":[{"id":"` +
		qwenModelPathFixture +
		`","object":"model","owned_by":"llamacpp","meta":{` +
		`"n_params":1881825088}}],"models":[{"name":"` +
		qwenModelPathFixture +
		`","model":"` +
		qwenModelPathFixture +
		`"}]}`
	tests := []struct {
		name       string
		payload    string
		statusCode int
		provenance adaptiveBenchmarkQwenHostProvenance
		inspectErr error
	}{
		{
			name:       "http status",
			payload:    `{}`,
			statusCode: http.StatusServiceUnavailable,
		},
		{
			name:    "malformed JSON",
			payload: `{`,
		},
		{
			name: "artifact basename",
			payload: strings.ReplaceAll(
				valid,
				auraeval.AdaptiveBenchmarkQwenArtifactName,
				"other.gguf",
			),
		},
		{
			name: "repository",
			payload: strings.ReplaceAll(
				valid,
				"models--unsloth--Qwen3.5-2B-GGUF",
				"models--other--Qwen3.5-2B-GGUF",
			),
		},
		{
			name: "snapshot",
			payload: strings.ReplaceAll(
				valid,
				"f6d5376be1edb4d416d56da11e5397a961aca8ae",
				"not-a-revision",
			),
		},
		{
			name: "parameter count",
			payload: strings.Replace(
				valid,
				"1881825088",
				"1",
				1,
			),
		},
		{
			name:       "inspect failure",
			payload:    valid,
			inspectErr: errors.New("inspect failed"),
		},
		{
			name:    "model hash",
			payload: valid,
			provenance: adaptiveBenchmarkQwenHostProvenance{
				ArtifactSHA256: strings.Repeat("0", 64),
				ServerVersion:  "b9994",
				ServerRevision: "14d3ba45f",
				ImageDigest: "sha256:" +
					strings.Repeat("1", 64),
			},
		},
		{
			name:    "server revision",
			payload: valid,
			provenance: adaptiveBenchmarkQwenHostProvenance{
				ArtifactSHA256: auraeval.AdaptiveBenchmarkQwenArtifactSHA256,
				ServerVersion:  "",
				ServerRevision: "",
				ImageDigest: "sha256:" +
					strings.Repeat("1", 64),
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			statusCode := testCase.statusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			doer := adaptiveBenchmarkHTTPDoerFunc(func(
				*http.Request,
			) (*http.Response, error) {
				response := qwenModelsResponse(testCase.payload)
				response.StatusCode = statusCode
				return response, nil
			})
			provenance := testCase.provenance
			if provenance == (adaptiveBenchmarkQwenHostProvenance{}) &&
				testCase.inspectErr == nil {
				provenance = validAdaptiveBenchmarkQwenHostProvenance()
			}
			inspector := &adaptiveBenchmarkQwenInspectorFake{
				provenance: provenance,
				err:        testCase.inspectErr,
			}
			if model, err := preflightAdaptiveBenchmarkQwen(
				t.Context(),
				doer,
				inspector,
			); err == nil {
				t.Fatalf("accepted model %#v", model)
			}
		})
	}
}

func qwenModelsResponse(payload string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(payload)),
		Header:     make(http.Header),
	}
}

func validAdaptiveBenchmarkQwenHostProvenance() adaptiveBenchmarkQwenHostProvenance {
	return adaptiveBenchmarkQwenHostProvenance{
		ArtifactSHA256: auraeval.AdaptiveBenchmarkQwenArtifactSHA256,
		ServerVersion:  "b9994",
		ServerRevision: "14d3ba45f",
		ImageDigest: "sha256:" +
			"b57dce073940d6f347d59230d4d28fc947db8948f2eb162326008380b07bab77",
	}
}

type adaptiveBenchmarkHTTPDoerFunc func(
	*http.Request,
) (*http.Response, error)

func (function adaptiveBenchmarkHTTPDoerFunc) Do(
	request *http.Request,
) (*http.Response, error) {
	return function(request)
}

type adaptiveBenchmarkQwenInspectorFake struct {
	provenance adaptiveBenchmarkQwenHostProvenance
	path       string
	err        error
}

func (fake *adaptiveBenchmarkQwenInspectorFake) Inspect(
	_ context.Context,
	path string,
) (adaptiveBenchmarkQwenHostProvenance, error) {
	fake.path = path
	return fake.provenance, fake.err
}
