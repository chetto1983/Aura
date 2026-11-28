package cron

import "testing"

// An agent_job's report is generated text that may relay what its run read, and it reaches a live
// turn inside the untrusted envelope its steer copy is delivered in. Its record goes through the
// report seam, which writes it the same way, so a later turn reloading it meets the same envelope.
// A reminder (the operator's own words) and a watch (whose record frames the mail already) do not.
func TestOnlyAnAgentJobRecordsItsReportThroughTheReportSeam(t *testing.T) {
	t.Parallel()
	for kind, wantReport := range map[TaskKind]bool{KindAgentJob: true, KindReminder: false, KindPIMWatch: false} {
		got := dispatchRecorded(t, kind, nil).appended[0]
		if isReport := got.report != ""; isReport != wantReport {
			t.Errorf("%s recorded through the report seam = %v, want %v", kind, isReport, wantReport)
		}
		if wantReport && got.report != "Nessuna anomalia rilevata" {
			t.Errorf("%s report body = %q, want the run's summary without its author line", kind, got.report)
		}
	}
}
