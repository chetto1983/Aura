package conversations

import "testing"

func TestCompactionMetricBoundedAndRedacted(t *testing.T) {
	s := &metricCapture{}
	e := CompactionMetric{Trigger: "proactive", Reason: "threshold", ProviderClass: "openai_compat", Rollout: "shadow", Quality: "not_evaluated", TokensBefore: 10, TokensAfter: 5}
	if err := EmitCompactionMetric(s, e); err != nil {
		t.Fatal(err)
	}
	if len(s.events) != 1 {
		t.Fatal("missing event")
	}
	e.Trigger = "conversation-id"
	if err := EmitCompactionMetric(s, e); err == nil {
		t.Fatal("unbounded trigger accepted")
	}
}

type metricCapture struct{ events []CompactionMetric }

func (m *metricCapture) Observe(e CompactionMetric) { m.events = append(m.events, e) }
