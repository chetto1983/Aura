package main

import (
	"context"
	"testing"
)

func TestMemoryRecallPeriodInputValidation(t *testing.T) {
	for name, input := range map[string]MemoryRecallInput{
		"bad start":            {Mode: "period", From: "yesterday", To: "2026-10-06T00:00:00+02:00"},
		"bad end":              {Mode: "period", From: "2026-10-05T00:00:00+02:00", To: "today"},
		"missing bounds":       {Mode: "period"},
		"reasoning selector":   {Mode: "period", TraceID: "trace-one"},
		"reasoning date range": {Mode: "reasoning", TraceID: "trace-one", From: "2026-10-05T00:00:00+02:00"},
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := newRecordingDB(t, `{"result":[]}`)
			_, _, err := memoryRecallHandler(singleTenant(t, client))(context.Background(), reqWithIdentity(testIdentity), input)
			if err == nil || len(rec.statements) != 0 {
				t.Fatalf("invalid period reached DB: error=%v, statements=%v", err, rec.statements)
			}
		})
	}
}

func TestMemoryRecallPeriodMCPAcceptsOffsetBoundaries(t *testing.T) {
	client, _ := newRecordingDB(t, `{"result":[]}`)
	_, output, err := memoryRecallHandler(singleTenant(t, client))(context.Background(), reqWithIdentity(testIdentity), MemoryRecallInput{
		Mode: "period", From: "2026-10-05T00:00:00+02:00", To: "2026-10-06T00:00:00+02:00",
	})
	if err != nil || !output.Abstained || output.Reason != "no_conversations_in_period" {
		t.Fatalf("offset-bound period = %+v, %v", output, err)
	}
}
