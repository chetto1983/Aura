package arcadedb

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func periodTestRequest() RecallRequest {
	return RecallRequest{
		IdentityID: "identity-a", Mode: RecallModePeriod,
		From:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.FixedZone("Berlin", 2*3600)),
		To:    time.Date(2026, 10, 6, 0, 0, 0, 0, time.FixedZone("Berlin", 2*3600)),
		Limit: 2,
	}
}

func TestMemoryRecallPeriodRejectsInvalidSelectorsBeforeQuery(t *testing.T) {
	for name, mutate := range map[string]func(*RecallRequest){
		"missing start":  func(r *RecallRequest) { r.From = time.Time{} },
		"missing end":    func(r *RecallRequest) { r.To = time.Time{} },
		"reversed":       func(r *RecallRequest) { r.From, r.To = r.To, r.From },
		"empty":          func(r *RecallRequest) { r.To = r.From },
		"query":          func(r *RecallRequest) { r.Query = "reminders" },
		"entity":         func(r *RecallRequest) { r.Entity = "Aura" },
		"predicate":      func(r *RecallRequest) { r.Predicate = "uses" },
		"as of":          func(r *RecallRequest) { r.AsOf = r.From },
		"conversation":   func(r *RecallRequest) { r.ConversationID = "one" },
		"anchor":         func(r *RecallRequest) { r.AnchorSeq = 1 },
		"direction":      func(r *RecallRequest) { r.Direction = RecallDirectionBefore },
		"cursor":         func(r *RecallRequest) { r.Cursor = "anything" },
		"semantic dates": func(r *RecallRequest) { r.Mode = RecallModeSemantic },
		"recent dates":   func(r *RecallRequest) { r.Mode = RecallModeRecent },
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			request := periodTestRequest()
			mutate(&request)
			if _, err := client.RecallMemory(context.Background(), request); err == nil {
				t.Fatal("invalid selector accepted")
			}
			if len(rec.statements) != 0 {
				t.Fatal("invalid selector queried the database")
			}
		})
	}
}

func TestMemoryRecallPeriodCursorIsolation(t *testing.T) {
	r := periodTestRequest()
	valid := RecallCursor{
		Version: recallCursorVersion, Mode: RecallModePeriod, IdentityID: r.IdentityID,
		From: r.From.Format(time.RFC3339Nano), To: r.To.Format(time.RFC3339Nano),
		PageSize: 2, ConversationID: "conversation-a", AnchorSeq: 7, AfterMillis: r.From.UnixMilli(),
	}
	for name, mutate := range map[string]func(*RecallRequest, *RecallCursor){
		"foreign identity":   func(_ *RecallRequest, c *RecallCursor) { c.IdentityID = "identity-b" },
		"changed start":      func(r *RecallRequest, _ *RecallCursor) { r.From = time.Now() },
		"changed end":        func(r *RecallRequest, _ *RecallCursor) { r.To = time.Now() },
		"changed page":       func(r *RecallRequest, _ *RecallCursor) { r.Limit = 3 },
		"query":              func(r *RecallRequest, _ *RecallCursor) { r.Query = "test" },
		"checkpoint outside": func(_ *RecallRequest, c *RecallCursor) { c.AfterMillis-- },
		"checkpoint end":     func(_ *RecallRequest, c *RecallCursor) { c.AfterMillis = r.To.UnixMilli() },
		"RID":                func(_ *RecallRequest, c *RecallCursor) { c.ConversationID = "#2:1" },
		"oversized page":     func(_ *RecallRequest, c *RecallCursor) { c.PageSize = 101 },
		"wrong version":      func(_ *RecallRequest, c *RecallCursor) { c.Version++ },
		"bad bounds":         func(_ *RecallRequest, c *RecallCursor) { c.From = "yesterday" },
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			cursor := valid
			request := RecallRequest{IdentityID: r.IdentityID, Mode: RecallModeScroll}
			mutate(&request, &cursor)
			raw, _ := json.Marshal(cursor)
			request.Cursor = encodeTestRecallCursor(t, cursor)
			if _, err := client.RecallMemory(context.Background(), request); err == nil {
				t.Fatalf("invalid period cursor accepted: %s", raw)
			}
			if len(rec.statements) != 0 {
				t.Fatal("invalid period cursor queried the database")
			}
		})
	}
}

func TestMemoryRecallPeriodEmptyAndSubmillisecondBounds(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	r := periodTestRequest()
	r.From = r.From.Add(time.Nanosecond)
	r.To = r.To.Add(time.Nanosecond)
	result, err := client.RecallMemory(context.Background(), r)
	if err != nil || !result.Abstained || result.NextCursor != "" || result.Reason != "no_conversations_in_period" {
		t.Fatalf("empty period = %+v, %v", result, err)
	}
	if rec.params[0]["period_from"] != float64(r.From.UnixMilli()+1) ||
		rec.params[0]["period_to"] != float64(r.To.UnixMilli()+1) {
		t.Fatalf("submillisecond bounds rounded incorrectly: %v", rec.params[0])
	}
	if !strings.Contains(rec.statements[0], "occurred_at < date(:period_to)") {
		t.Fatal("period is not half-open")
	}
}

func periodTestRows(request RecallRequest, seqs ...int) string {
	rows := make([]map[string]any, 0, len(seqs))
	for _, seq := range seqs {
		rows = append(rows, map[string]any{
			"identity_id": request.IdentityID, "conversation_id": "conversation-a", "turn_seq": seq,
			"role": "user", "content": "Repeated test", "content_hash": "fixture",
			"occurred_millis": request.From.UnixMilli(),
			"source_ref":      fmt.Sprintf("postgres://aura/conversations/conversation-a/turns/%d", seq),
		})
	}
	raw, _ := json.Marshal(map[string]any{"result": rows})
	return string(raw)
}

func TestMemoryRecallPeriodPagesRetainDistinctSourceTurns(t *testing.T) {
	r := periodTestRequest()
	client, rec := recordingClient(t, periodTestRows(r, 1, 7, 9), periodTestRows(r, 9, 12))
	first, err := client.RecallMemory(context.Background(), r)
	if err != nil || len(first.Evidence) != 1 || len(first.Evidence[0].Conversation.Turns) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	last, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: r.IdentityID, Mode: RecallModeScroll, Cursor: first.NextCursor,
	})
	if err != nil || last.NextCursor != "" || len(last.Evidence) != 1 {
		t.Fatalf("last page = %+v, %v", last, err)
	}
	var seqs []int
	for _, page := range []RecallResult{first, last} {
		for _, turn := range page.Evidence[0].Conversation.Turns {
			seqs = append(seqs, turn.Seq)
			if turn.OccurredAt != r.From.UTC().Format(time.RFC3339Nano) || turn.SourceRef == "" {
				t.Fatalf("timestamp or source lost: %+v", turn)
			}
		}
	}
	if !slices.Equal(seqs, []int{1, 7, 9, 12}) || rec.params[1]["after_seq"] != float64(7) {
		t.Fatalf("distinct turns or checkpoint lost: seqs=%v, params=%v", seqs, rec.params)
	}
}

func TestMemoryRecallPeriodRefusesIncompleteEvidence(t *testing.T) {
	r := periodTestRequest()
	for name, modify := range map[string]func(map[string]any){
		"foreign identity":      func(row map[string]any) { row["identity_id"] = "identity-b" },
		"excluded conversation": func(row map[string]any) { row["conversation_id"] = "conversation-active" },
		"missing source":        func(row map[string]any) { row["source_ref"] = "" },
		"outside interval":      func(row map[string]any) { row["occurred_millis"] = r.To.UnixMilli() },
		"invalid seq":           func(row map[string]any) { row["turn_seq"] = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			var body map[string][]map[string]any
			_ = json.Unmarshal([]byte(periodTestRows(r, 1)), &body)
			modify(body["result"][0])
			raw, _ := json.Marshal(body)
			client, _ := recordingClient(t, string(raw))
			request := r
			request.ExcludeConversationIDs = []string{"conversation-active"}
			if _, err := client.RecallMemory(context.Background(), request); err == nil {
				t.Fatal("incomplete or out-of-scope period claimed success")
			}
		})
	}
}
