package eval

import (
	"math"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
)

var adaptiveBenchmarkDomainOrder = []adaptive.Domain{
	adaptive.DomainReasoning,
	adaptive.DomainToolDiscovery,
	adaptive.DomainSkillRouting,
	adaptive.DomainKnowledge,
	adaptive.DomainMemoryRecall,
}

// BuildAdaptiveBenchmarkSummary deterministically aggregates registered records.
func BuildAdaptiveBenchmarkSummary(
	scenarios []AdaptiveBenchmarkScenarioRecord,
) AdaptiveBenchmarkSummary {
	summary := AdaptiveBenchmarkSummary{
		ScenarioCount: len(scenarios),
		Domains: make(
			[]AdaptiveBenchmarkDomainSummary,
			len(adaptiveBenchmarkDomainOrder),
		),
	}
	domainIndex := make(
		map[adaptive.Domain]int,
		len(adaptiveBenchmarkDomainOrder),
	)
	for index, domain := range adaptiveBenchmarkDomainOrder {
		domainIndex[domain] = index
		summary.Domains[index].Domain = domain
	}

	latencies := make([]int64, 0, len(scenarios))
	costTotal := float64(0)
	for _, scenario := range scenarios {
		summary.PromptTokens += scenario.PromptTokens
		summary.CompletionTokens += scenario.CompletionTokens
		summary.TotalTokens += scenario.TotalTokens
		switch scenario.Status {
		case AdaptiveBenchmarkStatusPass:
			summary.PassCount++
		case AdaptiveBenchmarkStatusFail:
			summary.FailCount++
		case AdaptiveBenchmarkStatusInvalid:
			summary.InvalidCount++
		}
		if index, ok := domainIndex[scenario.Domain]; ok {
			domain := &summary.Domains[index]
			domain.ScenarioCount++
			switch scenario.Status {
			case AdaptiveBenchmarkStatusPass:
				domain.PassCount++
			case AdaptiveBenchmarkStatusFail:
				domain.FailCount++
			case AdaptiveBenchmarkStatusInvalid:
				domain.InvalidCount++
			}
		}
		if scenario.LatencyNS > 0 {
			latencies = append(latencies, scenario.LatencyNS)
		}
		if scenario.CostUSD != nil {
			summary.CostCount++
			costTotal += *scenario.CostUSD
		}
	}
	slices.Sort(latencies)
	summary.LatencyCount = len(latencies)
	summary.LatencyP50NS = adaptiveBenchmarkNearestRank(latencies, 0.50)
	summary.LatencyP95NS = adaptiveBenchmarkNearestRank(latencies, 0.95)
	summary.LatencyP99NS = adaptiveBenchmarkNearestRank(latencies, 0.99)
	if summary.CostCount > 0 {
		summary.CostUSD = &costTotal
	}
	return summary
}

func adaptiveBenchmarkNearestRank(
	sorted []int64,
	percentile float64,
) *int64 {
	if len(sorted) == 0 {
		return nil
	}
	rank := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	value := sorted[max(0, min(rank, len(sorted)-1))]
	return &value
}

// ExpectedAdaptiveBenchmarkVerdict applies quality versus portability semantics.
func ExpectedAdaptiveBenchmarkVerdict(
	report AdaptiveBenchmarkReport,
) AdaptiveBenchmarkStatus {
	for _, scenario := range report.Scenarios {
		if scenario.Status == AdaptiveBenchmarkStatusInvalid {
			return AdaptiveBenchmarkStatusInvalid
		}
	}
	for _, control := range report.Controls {
		if control.Status == AdaptiveBenchmarkStatusInvalid {
			return AdaptiveBenchmarkStatusInvalid
		}
	}
	for _, control := range report.Controls {
		if control.Status == AdaptiveBenchmarkStatusFail {
			return AdaptiveBenchmarkStatusFail
		}
	}
	for _, scenario := range report.Scenarios {
		if scenario.Status != AdaptiveBenchmarkStatusFail {
			continue
		}
		if report.Mode == AdaptiveBenchmarkModeProductionShadow ||
			!adaptiveBenchmarkQualityOnlyReasons(scenario.ReasonCodes) {
			return AdaptiveBenchmarkStatusFail
		}
	}
	return AdaptiveBenchmarkStatusPass
}

func adaptiveBenchmarkQualityOnlyReasons(reasonCodes []string) bool {
	if len(reasonCodes) == 0 {
		return false
	}
	for _, reasonCode := range reasonCodes {
		switch reasonCode {
		case AdaptiveBenchmarkReasonAnswerMismatch,
			AdaptiveBenchmarkReasonToolMismatch,
			AdaptiveBenchmarkReasonSkillMismatch,
			AdaptiveBenchmarkReasonResultOrderMismatch:
		default:
			return false
		}
	}
	return true
}
