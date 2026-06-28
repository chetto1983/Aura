//go:build document_ingest_live || graphrag_live

package documents

import "time"

// livePercentile returns the nearest-rank percentile (p in [0,1]) of a duration
// sample (insertion sort), shared by the document_ingest_live and graphrag_live tiers.
func livePercentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[int(float64(len(sorted)-1)*p)]
}

// liveP95 is the p95 of a duration sample.
func liveP95(values []time.Duration) time.Duration { return livePercentile(values, 0.95) }
