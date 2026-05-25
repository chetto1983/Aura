// Package ctxmetrics exposes process-wide atomic counters and boot-time gauges
// for the context-engineering substrate (Phase-CTX). Values are read by the
// /api/metrics handler and exposed in Prometheus text format.
package ctxmetrics

import "sync/atomic"

// Counters tracks event counts for the two compaction layers.
type Counters struct {
	CTXCompactionsTotal        atomic.Int64 // AutoCompactEngine Compress calls that changed the message slice
	PayloadSummarizationsTotal atomic.Int64 // successful SubagentPayloadSummarizer reductions
	PayloadBreakerTripsTotal   atomic.Int64 // circuit-breaker trips (3 consecutive LLM failures)
	MicrocompactRunsTotal      atomic.Int64 // old tool-result microcompaction passes that cleared content
}

// Global is the process-wide counter set. Always non-nil; zero-initialized.
var Global = &Counters{}

// Gauges holds the effective compaction configuration resolved at boot.
// Written once by cmd/aura/helpers.go after populateMaxConversationTokens.
type Gauges struct {
	ModelContextWindow        int
	CompactionThresholdTokens int
}

// GlobalGauges holds the boot-time compaction configuration for the metrics endpoint.
var GlobalGauges = &Gauges{}
