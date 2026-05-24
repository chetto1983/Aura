package webadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/llm"
)

// ErrStreamAlreadyActive rejects concurrent SSE streams on the same web thread.
var ErrStreamAlreadyActive = errors.New("web stream: thread already active")

// StreamRouter is the web SSE outbound adapter for streaming chat runs.
type StreamRouter struct {
	mu      sync.Mutex
	streams map[string]*streamSink
}

var _ chat.OutboundAdapter = (*StreamRouter)(nil)

// NewStreamRouter constructs an empty streaming router for web SSE clients.
func NewStreamRouter() *StreamRouter {
	return &StreamRouter{streams: make(map[string]*streamSink)}
}

func (*StreamRouter) Channel() chat.Channel   { return chat.ChannelWeb }
func (*StreamRouter) Mode() chat.DeliveryMode { return chat.DeliveryModeStreaming }

func (r *StreamRouter) Begin(threadID string, w io.Writer, flush func()) (func(), error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, errors.New("web stream: thread id is required")
	}
	if w == nil {
		return nil, errors.New("web stream: writer is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.streams[threadID]; exists {
		return nil, ErrStreamAlreadyActive
	}
	r.streams[threadID] = newStreamSink(w, flush)
	return func() {
		r.mu.Lock()
		delete(r.streams, threadID)
		r.mu.Unlock()
	}, nil
}

func (r *StreamRouter) Deliver(_ context.Context, ev chat.OutboundEvent) error {
	if ev.ThreadID == "" {
		return nil
	}
	sink := r.sink(ev.ThreadID)
	if sink == nil {
		return nil
	}
	return sink.apply(ev)
}

// ConsumeTokens writes live LLM token deltas into the active SSE stream and
// returns the accumulated llm.Response expected by the agent loop.
func (r *StreamRouter) ConsumeTokens(ctx context.Context, threadID string, ch <-chan llm.Token) (llm.Response, bool, error) {
	sink := r.sink(threadID)
	if sink == nil {
		return drainTokenStream(ctx, ch)
	}
	return sink.consumeTokens(ctx, ch)
}

func (r *StreamRouter) sink(threadID string) *streamSink {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.streams[threadID]
}

type streamSink struct {
	mu          sync.Mutex
	w           io.Writer
	flush       func()
	messageID   string
	textID      string
	messageOpen bool
	textOpen    bool
	finished    bool
	usage       streamUsage
	toolOpen    map[string]struct{}
	textBuffer  strings.Builder
}

type streamUsage struct {
	inputTokens  int
	outputTokens int
	finishReason string
}

func newStreamSink(w io.Writer, flush func()) *streamSink {
	return &streamSink{
		w:        w,
		flush:    flush,
		toolOpen: make(map[string]struct{}),
		usage: streamUsage{
			finishReason: "stop",
		},
	}
}

func (s *streamSink) apply(ev chat.OutboundEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return nil
	}
	var err error
	switch ev.Type {
	case chat.EventRunStarted:
		err = s.ensureMessageStarted(ev)
	case chat.EventMessageDelta:
		err = s.writeTextDelta(ev)
	case chat.EventMessageDone:
		err = s.writeMessageDone(ev)
	case chat.EventToolStart:
		err = s.writeToolStart(ev)
	case chat.EventToolEnd:
		err = s.writeToolEnd(ev)
	case chat.EventQuestionRequested:
		err = s.writeQuestionRequested(ev)
	case chat.EventUsage:
		s.captureUsage(ev)
	case chat.EventError:
		s.usage.finishReason = "error"
		err = writeUIMessageChunk(s.w, map[string]any{
			"type":      "error",
			"errorText": stringPayload(ev.Payload, "error"),
		})
	case chat.EventCancelled:
		s.usage.finishReason = "error"
		err = writeUIMessageChunk(s.w, map[string]any{
			"type":      "error",
			"errorText": stringPayload(ev.Payload, "reason"),
		})
	case chat.EventDone:
		err = s.finish(ev)
	}
	if err != nil {
		return err
	}
	s.flushNow()
	return nil
}

func (s *streamSink) writeQuestionRequested(ev chat.OutboundEvent) error {
	q := pendingQuestionFromEvent(ev)
	if q == nil {
		return nil
	}
	if err := s.ensureMessageStarted(ev); err != nil {
		return err
	}
	return writeUIMessageChunk(s.w, map[string]any{
		"type": "data-pending-question",
		"id":   q.ID,
		"data": map[string]any{
			"id":       q.ID,
			"question": q.Question,
			"options":  q.Options,
			"kind":     q.Kind,
		},
	})
}

func (s *streamSink) consumeTokens(ctx context.Context, ch <-chan llm.Token) (llm.Response, bool, error) {
	var resp llm.Response
	var reasoning strings.Builder
	for {
		select {
		case <-ctx.Done():
			return resp, s.delivered(), ctx.Err()
		case tok, ok := <-ch:
			if !ok {
				resp.Content = s.text()
				resp.Reasoning = strings.TrimSpace(reasoning.String())
				return resp, s.delivered(), nil
			}
			if tok.Err != nil {
				return resp, s.delivered(), tok.Err
			}
			if tok.Reasoning != "" {
				reasoning.WriteString(tok.Reasoning)
			}
			if tok.Content != "" {
				if err := s.writeTextDeltaContent(tok.Content); err != nil {
					return resp, s.delivered(), err
				}
				s.flushNow()
			}
			if tok.Done {
				resp = llm.Response{
					Content:      s.text(),
					HasToolCalls: len(tok.ToolCalls) > 0,
					ToolCalls:    append([]llm.ToolCall(nil), tok.ToolCalls...),
					Usage:        tok.Usage,
					Reasoning:    strings.TrimSpace(reasoning.String()),
					EndTurn:      tok.EndTurn,
					FinishReason: tok.FinishReason,
				}
				return resp, s.delivered() && !resp.HasToolCalls, nil
			}
		}
	}
}

func (s *streamSink) ensureMessageStarted(ev chat.OutboundEvent) error {
	if s.messageOpen {
		return nil
	}
	s.messageOpen = true
	s.messageID = "msg_" + safeFrameID(ev.RunID)
	s.textID = "text_" + safeFrameID(ev.RunID)
	return writeUIMessageChunk(s.w, map[string]any{
		"type":      "start",
		"messageId": s.messageID,
	})
}

func (s *streamSink) ensureTextStarted(ev chat.OutboundEvent) error {
	if err := s.ensureMessageStarted(ev); err != nil {
		return err
	}
	if s.textOpen {
		return nil
	}
	s.textOpen = true
	return writeUIMessageChunk(s.w, map[string]any{
		"type": "text-start",
		"id":   s.currentTextID(),
	})
}

func (s *streamSink) writeTextDelta(ev chat.OutboundEvent) error {
	if ev.Content == "" {
		return nil
	}
	if current := s.textBuffer.String(); current != "" {
		switch {
		case ev.Content == current:
			return nil
		case strings.HasPrefix(ev.Content, current):
			ev.Content = strings.TrimPrefix(ev.Content, current)
			if ev.Content == "" {
				return nil
			}
		}
	}
	return s.writeTextDeltaEvent(ev)
}

func (s *streamSink) writeTextDeltaEvent(ev chat.OutboundEvent) error {
	if err := s.ensureTextStarted(ev); err != nil {
		return err
	}
	s.textBuffer.WriteString(ev.Content)
	return writeUIMessageChunk(s.w, map[string]any{
		"type":      "text-delta",
		"id":        s.currentTextID(),
		"textDelta": ev.Content,
		"delta":     ev.Content,
	})
}

func (s *streamSink) writeTextDeltaContent(content string) error {
	if content == "" {
		return nil
	}
	return s.writeTextDeltaEvent(chat.OutboundEvent{
		RunID:   strings.TrimPrefix(s.messageID, "msg_"),
		Content: content,
	})
}

func (s *streamSink) writeMessageDone(ev chat.OutboundEvent) error {
	if ev.Content != "" && !s.textOpen {
		if err := s.writeTextDelta(ev); err != nil {
			return err
		}
	}
	if !s.textOpen {
		return nil
	}
	s.textOpen = false
	return writeUIMessageChunk(s.w, map[string]any{
		"type": "text-end",
		"id":   s.currentTextID(),
	})
}

func (s *streamSink) writeToolStart(ev chat.OutboundEvent) error {
	if err := s.ensureMessageStarted(ev); err != nil {
		return err
	}
	callID := stringPayload(ev.Payload, "tool_call_id")
	if callID == "" {
		callID = fmt.Sprintf("tool_%s_%d", safeFrameID(ev.RunID), ev.Seq)
	}
	toolName := stringPayload(ev.Payload, "tool")
	if toolName == "" {
		toolName = "tool"
	}
	s.toolOpen[callID] = struct{}{}
	if err := writeUIMessageChunk(s.w, map[string]any{
		"type":       "tool-call-start",
		"id":         "part_" + safeFrameID(callID),
		"toolCallId": callID,
		"toolName":   toolName,
	}); err != nil {
		return err
	}
	if argsText := redactedArgsTextPayload(ev.Payload); argsText != "" {
		if err := writeUIMessageChunk(s.w, map[string]any{
			"type":     "tool-call-delta",
			"argsText": argsText,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *streamSink) writeToolEnd(ev chat.OutboundEvent) error {
	callID := stringPayload(ev.Payload, "tool_call_id")
	if callID == "" {
		callID = fmt.Sprintf("tool_%s_%d", safeFrameID(ev.RunID), ev.Seq)
	}
	if _, ok := s.toolOpen[callID]; !ok {
		if err := s.writeToolStart(chat.OutboundEvent{
			RunID:   ev.RunID,
			Seq:     ev.Seq,
			Payload: ev.Payload,
		}); err != nil {
			return err
		}
	}
	delete(s.toolOpen, callID)
	if err := writeUIMessageChunk(s.w, map[string]any{"type": "tool-call-end"}); err != nil {
		return err
	}
	success := boolPayload(ev.Payload, "success")
	result := map[string]any{
		"tool":       stringPayload(ev.Payload, "tool"),
		"success":    success,
		"elapsed_ms": intPayload(ev.Payload, "elapsed_ms"),
	}
	if preview := stringPayload(ev.Payload, "preview"); preview != "" {
		result["preview"] = preview
	}
	return writeUIMessageChunk(s.w, map[string]any{
		"type":       "tool-result",
		"toolCallId": callID,
		"result":     result,
		"isError":    !success,
	})
}

func (s *streamSink) captureUsage(ev chat.OutboundEvent) {
	s.usage.inputTokens = intPayload(ev.Payload, "tokens_prompt")
	s.usage.outputTokens = intPayload(ev.Payload, "tokens_completion")
	if reason := stringPayload(ev.Payload, "stop_reason"); reason != "" {
		s.usage.finishReason = finishReason(reason)
	}
}

func (s *streamSink) finish(ev chat.OutboundEvent) error {
	if s.textOpen {
		if err := s.writeMessageDone(ev); err != nil {
			return err
		}
	}
	if status := stringPayload(ev.Payload, "status"); status == string(chat.RunStatusFailed) || status == string(chat.RunStatusCancelled) {
		s.usage.finishReason = "error"
	}
	if err := writeUIMessageChunk(s.w, map[string]any{
		"type":         "finish",
		"finishReason": s.usage.finishReason,
		"usage": map[string]any{
			"inputTokens":  s.usage.inputTokens,
			"outputTokens": s.usage.outputTokens,
		},
	}); err != nil {
		return err
	}
	if err := writeUIMessageDone(s.w); err != nil {
		return err
	}
	s.finished = true
	return nil
}

func (s *streamSink) currentTextID() string {
	if s.textID != "" {
		return s.textID
	}
	return "text_unknown"
}

func (s *streamSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.textBuffer.String()
}

func (s *streamSink) delivered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.textBuffer.Len() > 0
}

func (s *streamSink) flushNow() {
	if s.flush != nil {
		s.flush()
	}
}

func drainTokenStream(ctx context.Context, ch <-chan llm.Token) (llm.Response, bool, error) {
	var sb strings.Builder
	var reasoning strings.Builder
	for {
		select {
		case <-ctx.Done():
			return llm.Response{Content: sb.String(), Reasoning: strings.TrimSpace(reasoning.String())}, false, ctx.Err()
		case tok, ok := <-ch:
			if !ok {
				return llm.Response{Content: sb.String(), Reasoning: strings.TrimSpace(reasoning.String())}, false, nil
			}
			if tok.Err != nil {
				return llm.Response{Content: sb.String(), Reasoning: strings.TrimSpace(reasoning.String())}, false, tok.Err
			}
			if tok.Content != "" {
				sb.WriteString(tok.Content)
			}
			if tok.Reasoning != "" {
				reasoning.WriteString(tok.Reasoning)
			}
			if tok.Done {
				return llm.Response{
					Content:      sb.String(),
					HasToolCalls: len(tok.ToolCalls) > 0,
					ToolCalls:    append([]llm.ToolCall(nil), tok.ToolCalls...),
					Usage:        tok.Usage,
					Reasoning:    strings.TrimSpace(reasoning.String()),
					EndTurn:      tok.EndTurn,
					FinishReason: tok.FinishReason,
				}, false, nil
			}
		}
	}
}

func finishReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "stop", "length", "content-filter", "tool-calls", "error", "other", "unknown":
		return reason
	case "":
		return "stop"
	default:
		return "other"
	}
}

func safeFrameID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func stringPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, _ := payload[key].(string)
	return v
}

func boolPayload(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	v, _ := payload[key].(bool)
	return v
}

func intPayload(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch v := payload[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func redactedArgsTextPayload(payload map[string]any) string {
	keys := stringSlicePayload(payload, "arg_keys")
	if len(keys) == 0 {
		return ""
	}
	args := make(map[string]string, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		args[key] = "[redacted]"
	}
	if len(args) == 0 {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(data)
}
