package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/chetto1983/aura/internal/elicit"
)

func TestARefusalNoticeIsRecordedBeforeAFastRunFinishes(t *testing.T) {
	_, srv, sess, live := openRun(t)
	sess.questions.NoticeRefusal(context.Background(), elicit.Question{
		Server: "forms", Tool: "ask_name", Refusal: elicit.RefusalAmbiguousRun,
		Message: "private-answer", Fields: []elicit.Field{{Name: "private-answer"}},
	})
	sess.finish()
	shown := nextCustom(t, live, ElicitationEventName).(elicitationFrame)
	resolved := nextCustom(t, live, ElicitationResolvedEventName).(elicitationResolvedFrame)
	if shown.RunID != sess.RunID || shown.ID == "" || shown.Server != "forms" || shown.Tool != "ask_name" ||
		shown.Refusal != elicit.RefusalAmbiguousRun || shown.Message != "" || shown.Fields != nil ||
		resolved.ID != shown.ID || resolved.Action != elicit.ActionDecline {
		t.Fatalf("live refusal frames = %+v, %+v", shown, resolved)
	}
	if listed := sess.questions.open(); len(listed) != 0 {
		t.Fatalf("refusal became an answerable question: %+v", listed)
	}
	if status, body := answerPost(t, srv, sess.RunID, shown.ID, `{"action":"accept","content":{}}`); status != http.StatusGone {
		t.Fatalf("POST to the finished run = %d %s, want 410", status, body)
	}

	replay, cancel, ok := sess.subscribeFrom(0)
	defer cancel()
	if !ok {
		t.Fatal("fast-finished run lost its replay")
	}
	var names []string
	for sev := range replay {
		custom, isCustom := sev.Ev.(*events.CustomEvent)
		if !isCustom {
			continue
		}
		names = append(names, custom.Name)
		raw, err := json.Marshal(custom)
		if err != nil || strings.Contains(string(raw), "private-answer") {
			t.Fatalf("unsafe replay frame: %s, %v", raw, err)
		}
	}
	if len(names) != 2 || names[0] != ElicitationEventName || names[1] != ElicitationResolvedEventName {
		t.Fatalf("fast-finished replay = %v, want question then decline", names)
	}
}

func TestARefusalNoticeCannotBeAcceptedWhileTheRunIsOpen(t *testing.T) {
	_, srv, sess, live := openRun(t)
	sess.questions.NoticeRefusal(context.Background(), elicit.Question{
		Server: "forms", Refusal: elicit.RefusalUnrenderable,
	})
	shown := nextCustom(t, live, ElicitationEventName).(elicitationFrame)
	if status, body := answerPost(t, srv, sess.RunID, shown.ID, `{"action":"accept","content":{}}`); status != http.StatusConflict {
		t.Fatalf("POST accepted a refusal = %d %s, want 409", status, body)
	}
	if resolved := nextCustom(t, live, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.ID != shown.ID || resolved.Action != elicit.ActionDecline {
		t.Fatalf("resolved = %+v", resolved)
	}
}
