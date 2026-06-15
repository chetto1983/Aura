package panicobs

import (
	"expvar"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Recovery-site labels for Record and Count: low-cardinality Prometheus/expvar
// label values naming where a panic was recovered. siteUnknown is the fallback
// for any site not in allowedSites, keeping label cardinality bounded.
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

// Record increments the recovered-panic counter (expvar + Prometheus) for site,
// normalizing any unrecognized site to siteUnknown to bound label cardinality.
func Record(site string) {
	site = normalizeSite(site)
	expRecoveredPanics.Add(site, 1)
	promRecoveredPanics.WithLabelValues(site).Inc()
}

// Count returns the number of recovered panics recorded for site (expvar-backed),
// or 0 if none; the site is normalized before lookup.
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
