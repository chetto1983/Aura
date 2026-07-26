package eval

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
)

func TestAdaptiveBenchmarkAdmissionFingerprintRejectsNoncanonicalTypedChildren(
	t *testing.T,
) {
	t.Parallel()
	base := benchmarkAdmissionRequest(t, benchmarkCohort(t))
	tests := []struct {
		name   string
		mutate func(*AdaptiveBenchmarkAdmissionRequest)
	}{
		{
			name: "missing typed fields",
			mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
				setBenchmarkAdmissionChild(
					request,
					0,
					[]byte(
						`{"schema_id":"aura.adaptive.interference-plan/v1","revision":1}`,
					),
				)
			},
		},
		{
			name: "unknown typed field",
			mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
				setBenchmarkAdmissionChild(
					request,
					0,
					[]byte(
						`{"schema_id":"aura.adaptive.interference-plan/v1","revision":1,"unknown":true}`,
					),
				)
			},
		},
		{
			name: "reordered typed fields",
			mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
				setBenchmarkAdmissionChild(
					request,
					0,
					[]byte(
						`{"revision":1,"schema_id":"aura.adaptive.interference-plan/v1"}`,
					),
				)
			},
		},
		{
			name: "noncanonical typed bytes",
			mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
				setBenchmarkAdmissionChild(
					request,
					0,
					[]byte(
						`{"schema_id": "aura.adaptive.interference-plan/v1","revision":1}`,
					),
				)
			},
		},
		{
			name: "artifact order",
			mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
				request.Artifacts[0], request.Artifacts[1] =
					request.Artifacts[1], request.Artifacts[0]
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := cloneBenchmarkAdmissionRequest(base)
			testCase.mutate(&request)
			if _, err := AdaptiveBenchmarkAdmissionFingerprint(
				request,
			); !errors.Is(err, ErrAdaptiveBenchmarkAdmissionInput) {
				t.Fatalf(
					"fingerprint error = %v, want admission input rejection",
					err,
				)
			}
		})
	}
}

func setBenchmarkAdmissionChild(
	request *AdaptiveBenchmarkAdmissionRequest,
	index int,
	canonical []byte,
) {
	request.Artifacts[index] = adaptive.EvidenceArtifactRegistration{
		Ref:       request.Artifacts[index].Ref,
		Canonical: canonical,
	}
	request.Artifacts[index].Ref.ArtifactSHA256 = benchmarkSHA256(canonical)
}
