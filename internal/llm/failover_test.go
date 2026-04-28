package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockProvider implements Client for testing failover.
type mockProvider struct {
	response Response
	err      error
	delay    time.Duration
}

func (m *mockProvider) Send(ctx context.Context, req Request) (Response, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return m.response, m.err
}

func (m *mockProvider) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan Token, 2)
	ch <- Token{Content: m.response.Content}
	ch <- Token{Done: true}
	close(ch)
	return ch, nil
}

func TestFailoverClientFirstProviderSucceeds(t *testing.T) {
	primary := &mockProvider{
		response: Response{Content: "primary response"},
	}
	backup := &mockProvider{
		response: Response{Content: "backup response"},
	}

	client, err := NewFailoverClient([]Client{primary, backup}, []string{"primary", "backup"})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Send(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "primary response" {
		t.Errorf("Send() = %q, want %q", resp.Content, "primary response")
	}
}

func TestFailoverClientFallbackOnPrimaryError(t *testing.T) {
	primary := &mockProvider{
		err: errors.New("primary failed"),
	}
	backup := &mockProvider{
		response: Response{Content: "backup response"},
	}

	client, err := NewFailoverClient([]Client{primary, backup}, []string{"primary", "backup"})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Send(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "backup response" {
		t.Errorf("Send() = %q, want %q", resp.Content, "backup response")
	}
}

func TestFailoverClientAllProvidersFail(t *testing.T) {
	primary := &mockProvider{err: errors.New("primary failed")}
	backup := &mockProvider{err: errors.New("backup failed")}

	client, err := NewFailoverClient([]Client{primary, backup}, []string{"primary", "backup"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Send(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestFailoverClientNoProviders(t *testing.T) {
	_, err := NewFailoverClient(nil, nil)
	if err == nil {
		t.Error("expected error when no providers provided")
	}
}

func TestFailoverClientStreamFallback(t *testing.T) {
	primary := &mockProvider{err: errors.New("stream failed")}
	backup := &mockProvider{response: Response{Content: "backup stream"}}

	client, err := NewFailoverClient([]Client{primary, backup}, []string{"primary", "backup"})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := client.Stream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}

	var result string
	for token := range ch {
		if token.Done {
			break
		}
		result += token.Content
	}
	if result != "backup stream" {
		t.Errorf("Stream() = %q, want %q", result, "backup stream")
	}
}

func TestFailoverClientNamesPadding(t *testing.T) {
	primary := &mockProvider{response: Response{Content: "ok"}}
	client, err := NewFailoverClient([]Client{primary}, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if client.names[0] != "provider_0" {
		t.Errorf("expected default name 'provider_0', got %q", client.names[0])
	}
}

func TestOllamaClientCreation(t *testing.T) {
	client := NewOllamaClient(OllamaConfig{
		BaseURL: "http://localhost:11434/v1",
		Model:   "llama3",
	})
	if client == nil {
		t.Error("NewOllamaClient returned nil")
	}
}

func TestFailoverClientContextCancellation(t *testing.T) {
	// Both providers block — context should cancel before any succeed
	primary := &mockProvider{delay: 10 * time.Second}
	backup := &mockProvider{delay: 10 * time.Second}

	client, err := NewFailoverClient([]Client{primary, backup}, []string{"primary", "backup"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = client.Send(ctx, Request{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Error("expected error from context cancellation")
	}
}
