package mcptools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
)

type acknowledgingRefusalAsker struct {
	entered chan elicit.Question
	release <-chan struct{}
	asked   chan elicit.Question
}

func (a *acknowledgingRefusalAsker) Ask(_ context.Context, q elicit.Question) (elicit.Answer, error) {
	a.asked <- q
	return elicit.Answer{Action: elicit.ActionDecline}, nil
}

func (a *acknowledgingRefusalAsker) NoticeRefusal(_ context.Context, q elicit.Question) {
	a.entered <- q
	<-a.release
}

func TestRefuseWaitsForRunNoticeBeforeReturning(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	open := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(open)
	asker := &acknowledgingRefusalAsker{
		entered: make(chan elicit.Question, 1), release: release, asked: make(chan elicit.Question, 1),
	}
	ctx := elicit.WithAsker(context.Background(), asker)
	returned := make(chan elicitOutcome, 1)
	go func() {
		returned <- refuse(ctx, route{calls: []context.Context{ctx}}, nil,
			elicit.Question{Server: "fixture", Tool: "ask", Message: "private server text"},
			elicit.RefusalAmbiguousRun, time.Second)
	}()

	select {
	case notice := <-asker.entered:
		if notice.Server != "fixture" || notice.Tool != "ask" || notice.Refusal != elicit.RefusalAmbiguousRun ||
			notice.Message != "" || notice.Fields != nil {
			t.Fatalf("unsafe refusal notice: %+v", notice)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("run notice seam was not called")
	}
	select {
	case <-returned:
		t.Fatal("MCP decline returned before the run recorded its refusal notice")
	case <-time.After(25 * time.Millisecond):
	}
	open()
	select {
	case out := <-returned:
		if out.action != elicit.ActionDecline {
			t.Fatalf("refusal action = %q", out.action)
		}
	case <-time.After(time.Second):
		t.Fatal("refusal did not return after the run acknowledged the notice")
	}
	select {
	case q := <-asker.asked:
		t.Fatalf("run was asked to answer a refusal: %+v", q)
	default:
	}
}

func TestRefuseBoundsAnUncooperativeRunNotice(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	asker := &acknowledgingRefusalAsker{
		entered: make(chan elicit.Question, 1), release: release, asked: make(chan elicit.Question, 1),
	}
	ctx := elicit.WithAsker(context.Background(), asker)
	returned := make(chan elicitOutcome, 1)
	go func() {
		returned <- refuse(ctx, route{calls: []context.Context{ctx}}, nil,
			elicit.Question{Server: "fixture", Tool: "ask"},
			elicit.RefusalUnrenderable, 100*time.Millisecond)
	}()
	select {
	case out := <-returned:
		if out.action != elicit.ActionDecline {
			t.Fatalf("refusal action = %q", out.action)
		}
	case <-time.After(time.Second):
		t.Fatal("uncooperative notice held the declined call past its bound")
	}
	select {
	case <-asker.entered:
	default:
		t.Fatal("bounded notice seam was not attempted")
	}
}

func TestRoutedRefusalBoundsOversizedQuestionMetadata(t *testing.T) {
	t.Setenv(envMCPElicitationTimeoutSec, "1")
	logs := captureLogs(t, slog.LevelWarn)
	for _, tc := range []struct {
		name, server, tool, message string
		wantServer, wantTool        string
	}{
		{"oversized tool with nil schema", "fixture", strings.Repeat("🧭", elicit.MaxQuestionBytes/4+1), "Confirm?", "fixture", ""},
		{"oversized server with nil schema", strings.Repeat("界", elicit.MaxQuestionBytes/3+1), "ask", "Confirm?", "MCP server", "ask"},
		{"ordinary unicode names", "café", "ask_名前", strings.Repeat("m", elicit.MaxMessageBytes+1), "café", "ask_名前"},
		{"invalid utf8 metadata", "\xff", "\xfe", strings.Repeat("m", elicit.MaxMessageBytes+1), "MCP server", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asker := newFakeAsker(elicit.Answer{Action: elicit.ActionDecline}, 0)
			ctx := withCallTool(elicit.WithAsker(context.Background(), asker), tc.tool)
			start := logs.Len()
			res, err := NewElicitationHandler(tc.server, nil)(ctx, &sdkmcp.ElicitRequest{
				Params: &sdkmcp.ElicitParams{Mode: "form", Message: tc.message},
			})
			line := logs.String()[start:]
			if len(line) >= 512 {
				t.Fatalf("oversized configured server reached the resolved log: %d bytes", len(line))
			}
			if tc.server == "fixture" || tc.server == "café" {
				if !strings.Contains(line, "server="+tc.server) {
					t.Fatalf("normal configured server missing from log: %s", line)
				}
			}
			if err != nil || res.Action != elicit.ActionDecline || len(res.Content) != 0 {
				t.Fatalf("routed refusal = %+v, %v", res, err)
			}
			q := asker.question(t)
			if q.Server != tc.wantServer || q.Tool != tc.wantTool || q.Refusal != elicit.RefusalUnrenderable ||
				q.Message != "" || q.Fields != nil {
				t.Fatalf("unsafe routed notice: server %q, tool %q, refusal %q, message %q, fields %+v",
					q.Server, q.Tool, q.Refusal, q.Message, q.Fields)
			}
			if !utf8.ValidString(q.Server) || !utf8.ValidString(q.Tool) {
				t.Fatalf("notice metadata is invalid UTF-8: server %q, tool %q", q.Server, q.Tool)
			}
			wire, err := json.Marshal(struct {
				RunID string `json:"run_id"`
				elicit.Question
			}{RunID: "run-00000000-0000-0000-0000-000000000000", Question: q})
			if err != nil || len(wire) >= elicit.MaxQuestionBytes {
				t.Fatalf("refusal frame is %d bytes, over cap %d: %v", len(wire), elicit.MaxQuestionBytes, err)
			}
		})
	}
}
