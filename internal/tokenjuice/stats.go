package tokenjuice

import (
	"sort"
	"sync"
)

type globalStatsTracker struct {
	mu            sync.Mutex
	totalCalls    int64
	totalBytesIn  int64
	totalBytesOut int64
	ruleBytes     map[string]int64
}

var globalStats = &globalStatsTracker{ruleBytes: make(map[string]int64)}

// RecordCompaction updates process-level compaction stats. Safe for concurrent
// calls. Called by compactToolOutput in internal/agent whenever a rule fires.
func RecordCompaction(ruleID string, originalBytes, compactedBytes int) {
	saved := int64(originalBytes - compactedBytes)
	globalStats.mu.Lock()
	globalStats.totalCalls++
	globalStats.totalBytesIn += int64(originalBytes)
	globalStats.totalBytesOut += int64(compactedBytes)
	globalStats.ruleBytes[ruleID] += saved
	globalStats.mu.Unlock()
}

// RuleSavings pairs a rule ID with total bytes saved since process start.
type RuleSavings struct {
	RuleID     string `json:"rule_id"`
	BytesSaved int64  `json:"bytes_saved"`
}

// StatsSnapshot is a point-in-time view of process-level token-juice stats.
type StatsSnapshot struct {
	TotalCalls      int64          `json:"total_calls"`
	TotalBytesSaved int64          `json:"total_bytes_saved"`
	AvgRatio        float64        `json:"avg_ratio"`
	TopRules        []RuleSavings  `json:"top_rules_by_savings"`
}

// SnapshotStats returns a consistent read of all process-level stats. TopRules
// is sorted by bytes_saved descending and capped at 5.
func SnapshotStats() StatsSnapshot {
	globalStats.mu.Lock()
	calls := globalStats.totalCalls
	bytesIn := globalStats.totalBytesIn
	bytesOut := globalStats.totalBytesOut
	rules := make([]RuleSavings, 0, len(globalStats.ruleBytes))
	for id, saved := range globalStats.ruleBytes {
		rules = append(rules, RuleSavings{RuleID: id, BytesSaved: saved})
	}
	globalStats.mu.Unlock()

	var avgRatio float64
	if bytesIn > 0 {
		avgRatio = float64(bytesOut) / float64(bytesIn)
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].BytesSaved > rules[j].BytesSaved
	})
	if len(rules) > 5 {
		rules = rules[:5]
	}
	if rules == nil {
		rules = []RuleSavings{}
	}

	return StatsSnapshot{
		TotalCalls:      calls,
		TotalBytesSaved: bytesIn - bytesOut,
		AvgRatio:        avgRatio,
		TopRules:        rules,
	}
}

// ResetStats zeroes all process-level stats. Intended for test isolation only.
func ResetStats() {
	globalStats.mu.Lock()
	globalStats.totalCalls = 0
	globalStats.totalBytesIn = 0
	globalStats.totalBytesOut = 0
	globalStats.ruleBytes = make(map[string]int64)
	globalStats.mu.Unlock()
}
