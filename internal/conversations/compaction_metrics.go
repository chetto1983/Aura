package conversations

import "errors"

// ErrInvalidCompactionMetric rejects unbounded or unknown label values.
var ErrInvalidCompactionMetric = errors.New("invalid_compaction_metric")

// CompactionMetric contains structural numbers and bounded categorical labels only.
type CompactionMetric struct {
	Trigger, Reason, ProviderClass, Rollout, Quality string
	TokensBefore, TokensAfter, Generation            int
	LatencyMilliseconds                              int64
}

// CompactionMetricSink is the narrow telemetry output boundary.
type CompactionMetricSink interface{ Observe(CompactionMetric) }

var metricLabels = map[string]map[string]struct{}{
	"trigger":  setOf("proactive", "overflow", "manual", "idle", "boundary", "provider"),
	"reason":   setOf("threshold", "forecast", "disabled", "no_eligible_prefix", "provider_unsupported", "quality_rejected", "timeout", "context_unavailable"),
	"provider": setOf("openrouter", "llamacpp", "openai_compat", "unknown"),
	"rollout":  setOf("disabled", "shadow", "canary", "enabled"),
	"quality":  setOf("not_evaluated", "accepted", "rejected", "degraded"),
}

// EmitCompactionMetric validates bounded labels before emitting redacted telemetry.
func EmitCompactionMetric(s CompactionMetricSink, e CompactionMetric) error {
	if s == nil || !allowed("trigger", e.Trigger) || !allowed("reason", e.Reason) || !allowed("provider", e.ProviderClass) || !allowed("rollout", e.Rollout) || !allowed("quality", e.Quality) {
		return ErrInvalidCompactionMetric
	}
	s.Observe(e)
	return nil
}
func allowed(kind, value string) bool { _, ok := metricLabels[kind][value]; return ok }
func setOf(values ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}
	return m
}
