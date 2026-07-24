package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type report struct {
	SchemaVersion       string      `json:"schema_version"`
	RunID               string      `json:"run_id"`
	CreatedAt           time.Time   `json:"created_at"`
	Rows                int         `json:"rows"`
	DelayedRows         int         `json:"delayed_rows"`
	FirstWriteMS        float64     `json:"first_write_ms"`
	IdempotentWriteMS   float64     `json:"idempotent_write_ms"`
	BeforeDelayed       graphCounts `json:"before_delayed"`
	AfterDelayed        graphCounts `json:"after_delayed"`
	AfterIdempotent     graphCounts `json:"after_idempotent"`
	NeighborP50MS       float64     `json:"neighbor_p50_ms"`
	NeighborP95MS       float64     `json:"neighbor_p95_ms"`
	PromotedPolicy      string      `json:"promoted_policy"`
	RolledBackPolicy    string      `json:"rolled_back_policy"`
	MemoryHealthyBefore bool        `json:"memory_healthy_before"`
	MemoryHealthyAfter  bool        `json:"memory_healthy_after"`
	CleanupCounts       graphCounts `json:"cleanup_counts"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[SUMMARY] INVALIDATED error=%q\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	runID := "adaptive-099-" + time.Now().UTC().Format("20060102T150405Z")
	baselineID, candidateID := runID+"/static-v1", runID+"/graph-knn-v1"
	rows, err := loadDecisionRows(root, runID, candidateID)
	if err != nil {
		return err
	}
	delayed := 0
	for _, row := range rows {
		if row.Delayed {
			delayed++
		}
	}
	memoryBefore := tcpHealthy("127.0.0.1:8091")
	graph, err := openGraph(ctx, os.Getenv("NEO4J_PASSWORD"), runID)
	if err != nil {
		return err
	}
	defer func() { _ = graph.close(context.Background()) }()
	defer func() { _ = graph.cleanup(context.Background()) }()
	if err := graph.createSchema(ctx, len(rows[0].Embedding)); err != nil {
		return err
	}
	if err := graph.seedSnapshots(ctx, baselineID, candidateID); err != nil {
		return err
	}
	firstWrite, err := graph.writeRows(ctx, rows)
	if err != nil {
		return err
	}
	beforeDelayed, err := graph.counts(ctx)
	if err != nil {
		return err
	}
	if beforeDelayed.Decisions != int64(len(rows)) || beforeDelayed.Outcomes != int64(len(rows)-delayed) {
		return fmt.Errorf("pre-delay counts=%+v rows=%d delayed=%d", beforeDelayed, len(rows), delayed)
	}
	if err := graph.attachDelayed(ctx, rows); err != nil {
		return err
	}
	afterDelayed, err := graph.counts(ctx)
	if err != nil {
		return err
	}
	idempotentWrite, err := graph.writeRows(ctx, rows)
	if err != nil {
		return err
	}
	afterIdempotent, err := graph.counts(ctx)
	if err != nil {
		return err
	}
	if afterDelayed != afterIdempotent {
		return fmt.Errorf("idempotent rewrite changed counts: before=%+v after=%+v", afterDelayed, afterIdempotent)
	}
	latencies, err := graph.neighborLatencies(ctx, rows[0].Embedding, 100)
	if err != nil {
		return err
	}
	promoted, rolledBack, err := graph.promoteAndRollback(ctx, baselineID, candidateID)
	if err != nil {
		return err
	}
	memoryAfter := tcpHealthy("127.0.0.1:8091")
	if err := graph.cleanup(ctx); err != nil {
		return err
	}
	cleanupCounts, err := graph.counts(ctx)
	if err != nil {
		return err
	}
	out := report{
		SchemaVersion: "1.0", RunID: runID, CreatedAt: time.Now().UTC(),
		Rows: len(rows), DelayedRows: delayed,
		FirstWriteMS:      float64(firstWrite.Microseconds()) / 1000,
		IdempotentWriteMS: float64(idempotentWrite.Microseconds()) / 1000,
		BeforeDelayed:     beforeDelayed, AfterDelayed: afterDelayed, AfterIdempotent: afterIdempotent,
		NeighborP50MS: percentile(latencies, 0.50), NeighborP95MS: percentile(latencies, 0.95),
		PromotedPolicy: promoted, RolledBackPolicy: rolledBack,
		MemoryHealthyBefore: memoryBefore, MemoryHealthyAfter: memoryAfter,
		CleanupCounts: cleanupCounts,
	}
	dir := filepath.Join(root, ".planning", "spikes", "099-neo4j-outcome-policy-graph", "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "neo4j-outcome-graph.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s [METRIC] rows=%d delayed=%d write_ms=%.1f rewrite_ms=%.1f neighbor_p50_ms=%.2f neighbor_p95_ms=%.2f\n",
		time.Now().UTC().Format(time.RFC3339Nano), len(rows), delayed, out.FirstWriteMS,
		out.IdempotentWriteMS, out.NeighborP50MS, out.NeighborP95MS)
	fmt.Printf("%s [METRIC] counts=%+v promoted=%s rolled_back=%s memory_before=%t memory_after=%t cleanup=%+v\n",
		time.Now().UTC().Format(time.RFC3339Nano), afterIdempotent, promoted, rolledBack,
		memoryBefore, memoryAfter, cleanupCounts)
	if !memoryBefore || !memoryAfter {
		return fmt.Errorf("agent-memory health changed: before=%t after=%t", memoryBefore, memoryAfter)
	}
	if cleanupCounts != (graphCounts{}) {
		return fmt.Errorf("retention cleanup left nodes: %+v", cleanupCounts)
	}
	fmt.Printf("%s [SUMMARY] VALIDATED artifact=%s\n", time.Now().UTC().Format(time.RFC3339Nano), path)
	return nil
}

func tcpHealthy(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * p)
	return sorted[index]
}
