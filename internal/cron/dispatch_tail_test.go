package cron

import (
	"context"
	"testing"
)

func TestDispatchReminderFiresInQuietHours(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "take your meds"}
	comp := &fakeCompleter{}
	notif := &captureNotifier{}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{
		Store:      comp,
		Notifier:   notif,
		QuietHours: func(string) bool { return true },
	})

	task := Task{ID: "t6", Kind: KindReminder, Payload: []byte(`{"text":"take your meds"}`), NotifyRoute: "whatsapp"}
	if err := d.Dispatch(context.Background(), task, c); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 1 || notif.texts[0] != "take your meds" {
		t.Fatalf("an in-window reminder must still fire (D-23), got %v", notif.texts)
	}
}

func TestDispatchCompleterErrorIsNonFatal(t *testing.T) {
	t.Parallel()
	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "ok"}
	comp := &fakeCompleter{err: ErrAlreadyRunning}
	d, c := newDispatchFor(t, h, KindReminder, DispatchDeps{Store: comp, Notifier: &captureNotifier{}})
	if err := d.Dispatch(context.Background(), Task{ID: "t7", Kind: KindReminder}, c); err != nil {
		t.Fatalf("a completer error must not surface as a dispatch error, got %v", err)
	}
}

func TestDispatchDefaultsAlertThreshold(t *testing.T) {
	t.Parallel()
	d := NewDispatch(map[TaskKind]Handler{}, DispatchDeps{Store: &fakeCompleter{}})
	if d.deps.AlertThreshold == "" {
		t.Fatal("NewDispatch must default the alert threshold (Risky)")
	}
}
