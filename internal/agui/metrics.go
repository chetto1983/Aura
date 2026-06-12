package agui

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var promSSEDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "aura_agui_sse_dropped_total",
	Help: "Total non-lifecycle AG-UI SSE events dropped for slow clients.",
})

func recordSSEDropped() {
	promSSEDroppedTotal.Inc()
}
