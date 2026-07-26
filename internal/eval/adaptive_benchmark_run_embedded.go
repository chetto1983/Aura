package eval

import _ "embed"

//go:embed testdata/adaptive_benchmark_dataset.json
var frozenAdaptiveBenchmarkDatasetJSON []byte

// LoadFrozenAdaptiveBenchmarkDataset loads the registered runtime corpus.
func LoadFrozenAdaptiveBenchmarkDataset() (AdaptiveBenchmarkDataset, error) {
	return LoadAdaptiveBenchmarkDataset(frozenAdaptiveBenchmarkDatasetJSON)
}
