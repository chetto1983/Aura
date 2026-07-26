package eval

import (
	"slices"
	"testing"
)

func FuzzDecodeAdaptiveBenchmarkReportStrict(f *testing.F) {
	report := validAdaptiveBenchmarkReport(
		f,
		AdaptiveBenchmarkModeQwenPortability,
	)
	canonical, err := report.CanonicalJSON()
	if err != nil {
		f.Fatalf("CanonicalJSON: %v", err)
	}
	f.Add(canonical)
	f.Add(append([]byte(" "), canonical...))
	f.Add(append(canonical, byte('\n')))
	f.Fuzz(func(t *testing.T, payload []byte) {
		decoded, err := DecodeAdaptiveBenchmarkReport(payload)
		if err != nil {
			return
		}
		roundTrip, err := decoded.CanonicalJSON()
		if err != nil {
			t.Fatalf("accepted report no longer validates: %v", err)
		}
		if !slices.Equal(payload, roundTrip) {
			t.Fatal("accepted report is not byte canonical")
		}
	})
}
