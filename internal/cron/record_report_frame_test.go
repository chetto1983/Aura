package cron

import (
	"context"
	"errors"
	"testing"
)

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

// A failed push is Aura's own fact about the run, so its note goes to the seam beside the report,
// which records it after the envelope, never as part of the report inside it. The steer copy is
// one text and keeps the note at its end.
func TestAnAgentJobsDeliveryNoteStaysOutOfItsReport(t *testing.T) {
	t.Parallel()
	recorder := &fakeConversationRecorder{}
	pub := &fakeReportSteerer{}
	d := NewDispatch(map[TaskKind]Handler{KindAgentJob: &fakeHandler{summary: "report body"}}, DispatchDeps{
		Store: &fakeNotificationStore{}, ConversationRecorder: recorder, Steer: pub,
		Notifier: &captureNotifier{err: errors.New("Recipient must be provided")},
	})
	task := Task{ID: "task-1", Kind: KindAgentJob, NotifyRoute: "whatsapp", IdentityID: "id-1", OriginConversationID: "conv-1"}
	if err := d.Dispatch(context.Background(), task, &Claim{RunID: "run-1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	const note = "\n\nNot delivered via whatsapp: Recipient must be provided"
	if len(recorder.appended) != 1 || recorder.appended[0].report != "report body" || recorder.appended[0].note != note {
		t.Fatalf("recorded %+v, want the report and Aura's note apart", recorder.appended)
	}
	if len(pub.steered) != 1 || pub.steered[0].text != "[Scheduled task agent_job task-1]\nreport body"+note {
		t.Fatalf("steered %+v", pub.steered)
	}
}
