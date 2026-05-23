package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

type fakeMemoryJudgeClient struct {
	responses []string
	err       error
	requests  []llm.Request
}

func (f *fakeMemoryJudgeClient) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return llm.Response{}, f.err
	}
	if len(f.responses) == 0 {
		return llm.Response{Content: `{"memory":[]}`}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return llm.Response{Content: resp}, nil
}

func (f *fakeMemoryJudgeClient) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

func seedJudgeDoc(t *testing.T, store *memoryindex.Store, doc memoryindex.Document) memoryindex.Document {
	t.Helper()
	if doc.Kind == "" {
		doc.Kind = memoryindex.KindOperational
	}
	if doc.Handle == "" {
		doc.Handle = doc.ID
	}
	if doc.Status == "" {
		doc.Status = "active"
	}
	if doc.Priority == "" {
		doc.Priority = memoryindex.PriorityNormal
	}
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = time.Now().UTC()
	}
	if err := store.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("Upsert(%s): %v", doc.ID, err)
	}
	return doc
}

func seedJudgeLesson(t *testing.T, store *memoryindex.Store, id, tool, class, text string) Lesson {
	t.Helper()
	lesson := Lesson{ID: id, ToolName: tool, ErrorClass: class, Text: text, Priority: memoryindex.PriorityNormal}
	if lesson.ID == "" {
		lesson.ID = operationalLessonID(tool, class, text)
	}
	seedJudgeDoc(t, store, documentFromLesson(lesson, time.Now().UTC()))
	return lesson
}

func judgeDocs(t *testing.T, store *memoryindex.Store) []memoryindex.Document {
	t.Helper()
	docs, err := store.FetchRecentOperational(context.Background(), 100)
	if err != nil {
		t.Fatalf("FetchRecentOperational: %v", err)
	}
	return docs
}

func judgeDocByID(t *testing.T, store *memoryindex.Store, id string) (memoryindex.Document, bool) {
	t.Helper()
	for _, doc := range judgeDocs(t, store) {
		if doc.ID == id {
			return doc, true
		}
	}
	return memoryindex.Document{}, false
}

func TestResolveBatchEmptyExistingFastPathSkipsJudge(t *testing.T) {
	store := openPostHookStore(t)
	lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "Retry with a smaller page.")
	client := &fakeMemoryJudgeClient{}

	if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake", "")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("judge calls = %d, want 0", len(client.requests))
	}
	if _, ok := judgeDocByID(t, store, lesson.ID); !ok {
		t.Fatalf("new lesson was removed on empty-store fast path")
	}
}

func TestResolveBatchHashDedupDeletesNewAndSkipsJudge(t *testing.T) {
	store := openPostHookStore(t)
	seedJudgeDoc(t, store, memoryindex.Document{
		ID:    "operational:old",
		Title: "Lesson: web_fetch timeout",
		Body:  "Retry with a smaller page.",
	})
	lesson := seedJudgeLesson(t, store, "operational:new", "web_fetch", "timeout", "Retry with a smaller page.")
	client := &fakeMemoryJudgeClient{}

	if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake", "")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("judge calls = %d, want 0", len(client.requests))
	}
	if _, ok := judgeDocByID(t, store, lesson.ID); ok {
		t.Fatalf("duplicate new lesson still exists")
	}
}

func TestResolveBatchADDUpdatesNewTextAndUsesTemperatureZero(t *testing.T) {
	store := openPostHookStore(t)
	seedJudgeDoc(t, store, memoryindex.Document{ID: "operational:old", Title: "Lesson: web_fetch io", Body: "Old unrelated lesson."})
	lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "Retry with a smaller page.")
	client := &fakeMemoryJudgeClient{responses: []string{`{"memory":[{"id":"new:0","text":"Retry web_fetch with a smaller page before widening scope.","event":"ADD","old_memory":""}]}`}}

	if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake-model", "low")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(client.requests))
	}
	if client.requests[0].Temperature == nil || *client.requests[0].Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", client.requests[0].Temperature)
	}
	if client.requests[0].Model != "fake-model" || client.requests[0].ReasoningEffort != "low" {
		t.Fatalf("request model/reasoning = %q/%q", client.requests[0].Model, client.requests[0].ReasoningEffort)
	}
	doc, ok := judgeDocByID(t, store, lesson.ID)
	if !ok || !strings.Contains(doc.Body, "before widening scope") {
		t.Fatalf("new lesson not refined: ok=%v doc=%#v", ok, doc)
	}
}

func TestResolveBatchUPDATERewritesExistingAndRemovesSingleNew(t *testing.T) {
	store := openPostHookStore(t)
	existing := seedJudgeDoc(t, store, memoryindex.Document{ID: "operational:old", Title: "Lesson: web_fetch timeout", Body: "Retry once on timeout.", Priority: memoryindex.PriorityNormal})
	lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "Retry with a smaller page after timeout.")
	lesson.Priority = memoryindex.PriorityHigh
	client := &fakeMemoryJudgeClient{responses: []string{`{"memory":[{"id":"0","text":"On web_fetch timeout, retry with a smaller page before broadening the query.","event":"UPDATE","old_memory":"Retry once on timeout."}]}`}}

	if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake", "")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	doc, ok := judgeDocByID(t, store, existing.ID)
	if !ok || !strings.Contains(doc.Body, "smaller page") {
		t.Fatalf("existing not updated: ok=%v doc=%#v", ok, doc)
	}
	if doc.Priority != memoryindex.PriorityHigh {
		t.Fatalf("updated priority = %q, want high", doc.Priority)
	}
	if _, ok := judgeDocByID(t, store, lesson.ID); ok {
		t.Fatalf("new row should be removed after UPDATE")
	}
}

func TestResolveBatchDELETEAndNOOP(t *testing.T) {
	tests := []struct {
		name           string
		response       string
		wantExisting   bool
		wantNew        bool
		decisionTarget string
	}{
		{
			name:         "delete existing",
			response:     `{"memory":[{"id":"0","text":"","event":"DELETE","old_memory":"stale"}]}`,
			wantExisting: false,
			wantNew:      true,
		},
		{
			name:         "noop new",
			response:     `{"memory":[{"id":"new:0","text":"","event":"NOOP","old_memory":""}]}`,
			wantExisting: true,
			wantNew:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openPostHookStore(t)
			existing := seedJudgeDoc(t, store, memoryindex.Document{ID: "operational:old", Title: "Lesson: web_fetch timeout", Body: "stale"})
			lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "new lesson")
			client := &fakeMemoryJudgeClient{responses: []string{tt.response}}

			if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake", "")); err != nil {
				t.Fatalf("ResolveBatch: %v", err)
			}
			if _, ok := judgeDocByID(t, store, existing.ID); ok != tt.wantExisting {
				t.Fatalf("existing presence = %v, want %v", ok, tt.wantExisting)
			}
			if _, ok := judgeDocByID(t, store, lesson.ID); ok != tt.wantNew {
				t.Fatalf("new presence = %v, want %v", ok, tt.wantNew)
			}
		})
	}
}

func TestResolveBatchMalformedJSONKeepsOriginalAdd(t *testing.T) {
	store := openPostHookStore(t)
	seedJudgeDoc(t, store, memoryindex.Document{ID: "operational:old", Title: "Lesson: web_fetch timeout", Body: "old"})
	lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "new lesson")
	client := &fakeMemoryJudgeClient{responses: []string{"not json"}}

	if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake", "")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if _, ok := judgeDocByID(t, store, lesson.ID); !ok {
		t.Fatalf("malformed JSON should keep original ADD row")
	}
}

func TestResolveBatchSplitsAtTwenty(t *testing.T) {
	store := openPostHookStore(t)
	seedJudgeDoc(t, store, memoryindex.Document{ID: "operational:old", Title: "Lesson: web_fetch timeout", Body: "old"})
	var lessons []Lesson
	for i := 0; i < 21; i++ {
		lessons = append(lessons, seedJudgeLesson(t, store, "", "web_fetch", "timeout", fmt.Sprintf("new lesson %02d", i)))
	}
	client := &fakeMemoryJudgeClient{responses: []string{`{"memory":[]}`, `{"memory":[]}`}}

	if err := ResolveBatch(context.Background(), store, lessons, WithMemoryJudgeClient(client, "fake", "")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("judge calls = %d, want 2", len(client.requests))
	}
}

func TestResolveBatchTruncatesExistingCandidates(t *testing.T) {
	store := openPostHookStore(t)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		seedJudgeDoc(t, store, memoryindex.Document{
			ID:        fmt.Sprintf("operational:old-%02d", i),
			Title:     "Lesson: web_fetch timeout",
			Body:      "old candidate body",
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "new lesson")
	client := &fakeMemoryJudgeClient{responses: []string{`{"memory":[]}`}}

	if err := ResolveBatch(context.Background(), store, []Lesson{lesson}, WithMemoryJudgeClient(client, "fake", "")); err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(client.requests))
	}
	prompt := client.requests[0].Messages[1].Content
	if strings.Contains(prompt, "old-00") {
		t.Fatalf("prompt contains oldest candidate beyond top-50")
	}
}

func TestMemoryJudgeHookDisabledAndClientError(t *testing.T) {
	store := openPostHookStore(t)
	hook := MemoryJudgeHook{Enabled: false}
	if errs := hook.Apply(context.Background(), TurnRecord{OperationalWrites: []Lesson{{ToolName: "x", ErrorClass: "y", Text: "z"}}}, store); len(errs) != 0 {
		t.Fatalf("disabled hook errs = %v, want none", errs)
	}

	seedJudgeDoc(t, store, memoryindex.Document{ID: "operational:old", Title: "Lesson: web_fetch timeout", Body: "old"})
	lesson := seedJudgeLesson(t, store, "", "web_fetch", "timeout", "new lesson")
	hook = MemoryJudgeHook{Enabled: true, Client: &fakeMemoryJudgeClient{err: errors.New("boom")}}
	if errs := hook.Apply(context.Background(), TurnRecord{OperationalWrites: []Lesson{lesson}}, store); len(errs) != 1 {
		t.Fatalf("client error errs = %v, want one", errs)
	}
}
