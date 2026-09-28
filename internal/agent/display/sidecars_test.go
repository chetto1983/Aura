package display

import (
	"strings"
	"testing"
)

func calendarInput(action, result string) PreviewInput {
	return PreviewInput{ToolCallID: "cal-1", ToolName: "pim__calendar", Arguments: `{"action":"` + action + `"}`,
		ResultPreview: result, TrustedMCP: &TrustedMCP{Recipe: "recipe:calendar", Tool: "calendar", Action: action}}
}

func whatsappInput(tool, result string) PreviewInput {
	return PreviewInput{ToolCallID: "wa-1", ToolName: "wa__" + tool, ResultPreview: result,
		TrustedMCP: &TrustedMCP{Recipe: "recipe:whatsapp", Tool: tool}}
}

func TestSidecarReadShapes(t *testing.T) {
	cases := []struct {
		name          string
		in            PreviewInput
		columns, rows int
	}{
		{"calendar accounts", calendarInput("list_accounts", `{"accounts":[{"accountId":"a1","provider":"google","displayName":"Work"}]}`), 3, 1},
		{"calendar empty accounts", calendarInput("list_accounts", `{"accounts":[]}`), 3, 0},
		{"calendar emails", calendarInput("get_emails", `{"emails":[{"id":"e1","accountId":"a1","subject":"<b>hello</b>","from":"person@example.com","receivedDateTime":"2026-09-28T09:00:00Z","isRead":false,"hasAttachments":true}],"warnings":null}`), 5, 1},
		{"calendar events", calendarInput("get_calendar_events", `{"events":[{"eventId":"opaque","accountId":"a1","subject":"Meet","start_local":"2026-09-28T10:00:00","end_local":"2026-09-28T11:00:00","location":"Room"}],"warnings":null}`), 5, 1},
		{"calendar calendars", calendarInput("list_calendars", `{"calendars":[{"id":"primary","accountId":"a1","name":"Main","owner":"me","canEdit":true,"isDefault":true}]}`), 4, 1},
		{"calendar contacts", calendarInput("search_contacts", `{"contacts":[{"id":"c1","accountId":"a1","displayName":"Ada","emailAddresses":["ada@example.com"],"phoneNumbers":["123"]}]}`), 4, 1},
		{"whatsapp chats", whatsappInput("list_chats", `[{"jid":"123@s.whatsapp.net","name":"Ada","is_group":false,"last_message_time":"2026-09-28T10:00:00","last_message":"Hello"}]`), 4, 1},
		{"whatsapp contacts", whatsappInput("search_contacts", `[{"jid":"123@s.whatsapp.net","name":"Ada","phone_number":"123"}]`), 3, 1},
		{"whatsapp messages", whatsappInput("list_messages", `[{"id":"m1","timestamp":"2026-09-28T10:00:00","sender_display":"Ada (123)","chat_jid":"123@s.whatsapp.net","content":"Hello"}]`), 4, 1},
		{"whatsapp one chat", whatsappInput("get_chat", `{"jid":"123@s.whatsapp.net","name":"Ada","is_group":false,"last_message_time":null,"last_message":null}`), 4, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := NormalizeToolPreview(tc.in, NewRegistry())
			if !ok || p.Type != KindTable || p.Table == nil || len(p.Table.Columns) != tc.columns || len(p.Table.Rows) != tc.rows || p.ToolCallID != tc.in.ToolCallID {
				t.Fatalf("projection = %+v, ok=%v", p, ok)
			}
			if tc.name == "calendar emails" && p.Table.Rows[0][2] != "<b>hello</b>" {
				t.Fatalf("email subject changed: %v", p.Table.Rows[0])
			}
		})
	}
}

func TestSidecarReadsStayWithinTrustedShape(t *testing.T) {
	cases := []PreviewInput{
		calendarInput("send_email", `{"accounts":[{"accountId":"a1","provider":"google","displayName":"Work"}]}`),
		calendarInput("delete_email", `{"emails":[]}`),
		calendarInput("not_an_action", `{"accounts":[]}`),
		calendarInput("list_accounts", `{"accounts":{},"warnings":null}`),
		calendarInput("list_accounts", `{"accounts":[],"warnings":[{"accountId":"a1","error":"offline"}]}`),
		calendarInput("get_email_attachment", `{"path":"/container/file.pdf"}`),
		whatsappInput("send_message", `[{"jid":"123@s.whatsapp.net"}]`),
		whatsappInput("download_media", `{"path":"/container/audio.ogg"}`),
		whatsappInput("get_message_context", `{"message":{},"before":[],"after":[]}`),
		whatsappInput("list_chats", `[{"name":"Missing JID"}]`),
	}
	for _, in := range cases {
		if p, ok := NormalizeToolPreview(in, NewRegistry()); ok {
			t.Fatalf("%s/%s promoted: %+v", in.TrustedMCP.Recipe, in.TrustedMCP.Action, p)
		}
	}
	changed := calendarInput("list_accounts", `{"accounts":[]}`)
	changed.Arguments = `{"action":"send_email"}`
	if p, ok := NormalizeToolPreview(changed, NewRegistry()); ok {
		t.Fatalf("mismatched action promoted: %+v", p)
	}
	changed = whatsappInput("list_chats", `[]`)
	changed.TrustedMCP = nil
	if p, ok := NormalizeToolPreview(changed, NewRegistry()); ok {
		t.Fatalf("untrusted read promoted: %+v", p)
	}
	oversized := whatsappInput("list_chats", `[]`+strings.Repeat(" ", 70*1024))
	if p, ok := NormalizeToolPreview(oversized, NewRegistry()); ok {
		t.Fatalf("oversized read promoted: %+v", p)
	}
}
