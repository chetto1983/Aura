package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockClient implements Client for testing.
type mockClient struct {
	sendFn  func(ctx context.Context, req Request) (Response, error)
	streamFn func(ctx context.Context, req Request) (<-chan Token, error)
}

func (m *mockClient) Send(ctx context.Context, req Request) (Response, error) {
	if m.sendFn != nil {
		return m.sendFn(ctx, req)
	}
	return Response{Content: "mock response"}, nil
}

func (m *mockClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	ch := make(chan Token, 1)
	ch <- Token{Content: "mock", Done: true}
	close(ch)
	return ch, nil
}

func TestRetryClientSendSuccess(t *testing.T) {
	mock := &mockClient{}
	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	})

	resp, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "mock response" {
		t.Errorf("Content = %q, want %q", resp.Content, "mock response")
	}
}

func TestRetryClientSendRetries(t *testing.T) {
	calls := 0
	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			calls++
			if calls < 3 {
				return Response{}, errors.New("transient error")
			}
			return Response{Content: "success"}, nil
		},
	}
	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	})

	resp, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if resp.Content != "success" {
		t.Errorf("Content = %q, want %q", resp.Content, "success")
	}
}

func TestRetryClientSendExhausted(t *testing.T) {
	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			return Response{}, errors.New("permanent error")
		},
	}
	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 2,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   50 * time.Millisecond,
	})

	_, err := retry.Send(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if err.Error() != "permanent error" {
		t.Errorf("error = %q, want %q", err.Error(), "permanent error")
	}
}

func TestRetryClientStreamSuccess(t *testing.T) {
	mock := &mockClient{
		streamFn: func(ctx context.Context, req Request) (<-chan Token, error) {
			ch := make(chan Token, 3)
			ch <- Token{Content: "hel"}
			ch <- Token{Content: "lo"}
			ch <- Token{Done: true}
			close(ch)
			return ch, nil
		},
	}
	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 2,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   50 * time.Millisecond,
	})

	ch, err := retry.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result string
	for tok := range ch {
		if tok.Done {
			break
		}
		result += tok.Content
	}
	if result != "hello" {
		t.Errorf("result = %q, want %q", result, "hello")
	}
}

func TestRetryClientContextCanceled(t *testing.T) {
	mock := &mockClient{
		sendFn: func(ctx context.Context, req Request) (Response, error) {
			return Response{}, errors.New("fail")
		},
	}
	retry := NewRetryClient(mock, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  1 * time.Second,
		MaxDelay:   5 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := retry.Send(ctx, Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestBackoffDelay(t *testing.T) {
	retry := NewRetryClient(nil, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
	})

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{5, 30 * time.Second}, // capped at max
	}

	for _, tt := range tests {
		got := retry.backoffDelay(tt.attempt)
		if got != tt.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}