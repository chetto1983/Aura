package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

func TestAdaptiveBenchmarkRuntimeConfigEnablesFrozenMemoryRecall(
	t *testing.T,
) {
	cfg := &config.Config{
		MemoryRecall:         false,
		MemoryRecallMaxItems: 3,
	}
	if provider := dynamicRecallProvider(cfg, nil); provider != nil {
		t.Fatal("default config unexpectedly enables memory recall")
	}

	benchmarkCfg := adaptiveBenchmarkRuntimeConfig(cfg)

	if benchmarkCfg == cfg {
		t.Fatal("benchmark config aliases caller config")
	}
	if !benchmarkCfg.MemoryRecall ||
		benchmarkCfg.MemoryRecallMaxItems != 8 ||
		dynamicRecallProvider(benchmarkCfg, &memoryReadinessClient{}) == nil {
		t.Fatalf(
			"benchmark memory recall = %v/%d",
			benchmarkCfg.MemoryRecall,
			benchmarkCfg.MemoryRecallMaxItems,
		)
	}
	if cfg.MemoryRecall || cfg.MemoryRecallMaxItems != 3 {
		t.Fatalf(
			"caller memory recall mutated to %v/%d",
			cfg.MemoryRecall,
			cfg.MemoryRecallMaxItems,
		)
	}
}
