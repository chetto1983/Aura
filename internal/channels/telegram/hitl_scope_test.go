// hitl_scope_test.go covers the approval-SCOPE keyboard (PRD amendment #127) — the branch
// hitl.go takes when an approval pause carries options. Split out of hitl_test.go, which the
// 600-LOC cap would otherwise have pushed over.
package telegram

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
)

// TestApprovalScopeMarkupOffersTheScopesAndOneRefusal proves an approval carrying scope
// options reaches Telegram as one button per scope plus a refusal — and NOT as the fixed
// Sì/No keyboard, which answers with an empty content and would silently collapse the
// operator's choice to the narrowest scope (PRD amendment #127).
func TestApprovalScopeMarkupOffersTheScopesAndOneRefusal(t *testing.T) {
	t.Parallel()
	token := "01a02f2b-5a37-7597-b6ac-92368c6b56df"
	raw := json.RawMessage(`[{"label":"Approve once","value":"gateway_scope:once:calendar delete_event"},` +
		`{"label":"Approve calendar delete_event for this conversation","value":"gateway_scope:session:calendar delete_event"},` +
		`{"label":"Always approve calendar delete_event","value":"gateway_scope:always:calendar delete_event"},` +
		`{"label":"Some other choice","value":"not-a-scope"}]`)

	rows := approvalScopeMarkup(token, raw).InlineKeyboard
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 3 scopes + 1 unknown option + 1 refusal", len(rows))
	}
	// The gateway is not locale-aware: it ships a code plus an English fallback, and this
	// channel writes its own Italian. An option that is NOT a gateway scope keeps the label
	// the server sent — a raw code must never end up on a button.
	for i, want := range []string{
		"Approva una volta",
		"Approva calendar delete_event per questa conversazione",
		"Approva sempre calendar delete_event",
		"Some other choice",
	} {
		if got := rows[i][0].Text; got != want {
			t.Errorf("row %d = %q, want %q", i, got, want)
		}
		// The callback carries the option INDEX, not its text: Telegram caps callback_data
		// at 64 bytes and a scope label blows through it.
		if _, _, value, ok := parseCallback(rows[i][0].Data); !ok || value != strconv.Itoa(i) {
			t.Errorf("row %d callback value = %q (ok=%v), want the option index", i, value, ok)
		}
	}
	refusal := rows[4][0]
	if _, action, _, ok := parseCallback(refusal.Data); !ok || action != askuser.ActionDecline {
		t.Errorf("last row action = %q, want a decline", action)
	}
	// No generic accept: an "Approva" beside three that say how long they last would be the
	// easiest button to press and the one that says least.
	for _, row := range rows {
		for _, b := range row {
			if b.Text == "Approva" {
				t.Error("the scope keyboard offers a generic Approva beside the scoped ones")
			}
			if strings.HasPrefix(b.Text, "gateway_scope:") {
				t.Errorf("a raw wire code reached a button: %q", b.Text)
			}
		}
	}
}
