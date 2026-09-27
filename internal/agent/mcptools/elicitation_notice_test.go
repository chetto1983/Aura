package mcptools

import (
	"context"
	"sync"
	"testing"
	"time"

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
