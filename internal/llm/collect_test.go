package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type collectClient struct {
	chunks  []Chunk
	openErr error
	request Request
}

func (c *collectClient) Stream(_ context.Context, req Request) (<-chan Chunk, error) {
	c.request = req
	if c.openErr != nil {
		return nil, c.openErr
	}
	ch := make(chan Chunk, len(c.chunks))
	for _, chunk := range c.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestCollectReturnsTheTextAndTheUsage(t *testing.T) {
	client := &collectClient{chunks: []Chunk{
		{Text: "due "}, {Text: "parti", FinishReason: "stop"},
		{Usage: &Usage{PromptTokens: 12, CompletionTokens: 3}},
	}}

	text, usage, err := Collect(context.Background(), client, Request{Model: "m"})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if text != "due parti" || usage.PromptTokens != 12 || usage.CompletionTokens != 3 || client.request.Model != "m" {
		t.Fatalf("text %q, usage %+v, request %+v", text, usage, client.request)
	}
}

func TestCollectRefusesAStreamThatDidNotEndCleanly(t *testing.T) {
	broken := errors.New("reset by peer")
	for name, client := range map[string]*collectClient{
		"it never opened": {openErr: broken},
		"it broke":        {chunks: []Chunk{{Text: "metà"}, {Err: broken}}},
		"it ran out":      {chunks: []Chunk{{Text: "metà", FinishReason: "length"}}},
		"it just stopped": {chunks: []Chunk{{Text: "metà"}}},
	} {
		text, _, err := Collect(context.Background(), client, Request{})
		if err == nil || text != "" {
			t.Fatalf("%s: text %q, err %v", name, text, err)
		}
	}
}

func TestCollectNamesWhatWentWrong(t *testing.T) {
	broken := errors.New("reset by peer")
	_, _, err := Collect(context.Background(), &collectClient{chunks: []Chunk{{Err: broken}}}, Request{})
	if !errors.Is(err, broken) || !strings.HasPrefix(err.Error(), "stream: ") {
		t.Fatalf("err = %v", err)
	}
	_, _, err = Collect(context.Background(), &collectClient{chunks: []Chunk{{FinishReason: "length"}}}, Request{})
	if err == nil || err.Error() != `incomplete stream (finish_reason="length")` {
		t.Fatalf("err = %v", err)
	}
}
