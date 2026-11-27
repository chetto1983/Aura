package messagedrafts

import (
	"encoding/json"
	"strings"
	"testing"
)

func editedEmail(effective string) Draft {
	return Draft{
		Target:        emailTarget,
		OriginalArgs:  json.RawMessage(`{"accountId":"acc-1","to":["colleage@aura.test"],"subject":"Riunione","body":"alle 10"}`),
		EffectiveArgs: json.RawMessage(effective),
	}
}

// Measured 2026-11-27: the model read only the send's result, so it believed its own recipient and
// text had gone out after the operator rewrote both.
func TestOperatorEditsNameEveryChangedFieldWithWhatWasSent(t *testing.T) {
	t.Parallel()
	got := editedEmail(`{"accountId":"acc-1","to":["colleague@aura.test"],"cc":[],"subject":"Riunione","body":"alle <b>11</b>"}`).OperatorEdits()
	want := `The operator edited your message before approving it. These fields differ from your call: ` +
		`to = ["colleague@aura.test"]; body = "alle <b>11</b>".`
	if got != want {
		t.Fatalf("note = %q\nwant  %q", got, want)
	}
}

func TestOperatorEditsSayNothingWhenTheMessageIsTheModels(t *testing.T) {
	t.Parallel()
	for name, draft := range map[string]Draft{
		"no review edit":    {Target: emailTarget, OriginalArgs: json.RawMessage(`{"to":["a@example.com"]}`)},
		"the same fields":   editedEmail(`{"accountId":"acc-1","subject":"Riunione","to":["colleage@aura.test"],"body":"alle 10"}`),
		"an empty cc added": editedEmail(`{"accountId":"acc-1","to":["colleage@aura.test"],"cc":null,"subject":"Riunione","body":"alle 10"}`),
		"unreadable args":   {Target: emailTarget, OriginalArgs: json.RawMessage(`{`), EffectiveArgs: json.RawMessage(`{}`)},
	} {
		if note := draft.OperatorEdits(); note != "" {
			t.Errorf("%s: note = %q, want none", name, note)
		}
	}
}

func TestOperatorEditsCoverWhatsAppAndCutALongValue(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("è", editedValueLimit)
	draft := Draft{
		Target:        whatsAppTarget,
		OriginalArgs:  json.RawMessage(`{"recipient":"10000000000","message":"Original"}`),
		EffectiveArgs: json.RawMessage(`{"recipient":"20000000000","message":"` + long + `"}`),
	}
	note := draft.OperatorEdits()
	if !strings.Contains(note, `recipient = "20000000000"; message = "`) || !strings.HasSuffix(note, "….") {
		t.Fatalf("note = %q", note[:min(len(note), 200)])
	}
	if len(note) > 2*editedValueLimit {
		t.Fatalf("note is %d bytes, want the message cut near %d", len(note), editedValueLimit)
	}
}
