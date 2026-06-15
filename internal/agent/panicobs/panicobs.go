package panicobs

import (
	"expvar"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	SiteExecuteBatch     = "execute_batch"
	SiteLlmAgentRun      = "llm_agent_run"
	SiteWorkflowParallel = "workflow_parallel"
	SiteSwarmWave        = "swarm_wave"
	SiteShellBGReaper    = "shell_bg_reaper"
	siteUnknown          = "unknown"
)

var allowedSites = map[string]struct{}{
	SiteExecuteBatch:     {},
	SiteLlmAgentRun:      {},
	SiteWorkflowParallel: {},
	SiteSwarmWave:        {},
	SiteShellBGReaper:    {},
	siteUnknown:          {},
}

var (
	expRecoveredPanics  = expvar.NewMap("aura_agent_panic_total")
	promRecoveredPanics = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aura_agent_panic_total",
		Help: "Recovered agent panic count by low-cardinality site.",
	}, []string{"site"})
)

func Record(site string) {
	site = normalizeSite(site)
	expRecoveredPanics.Add(site, 1)
	promRecoveredPanics.WithLabelValues(site).Inc()
}

func Count(site string) int64 {
	if v := expRecoveredPanics.Get(normalizeSite(site)); v != nil {
		if i, ok := v.(*expvar.Int); ok {
			return i.Value()
		}
	}
	return 0
}

func normalizeSite(site string) string {
	site = strings.TrimSpace(site)
	if _, ok := allowedSites[site]; ok {
		return site
	}
	return siteUnknown
}
