package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockingCypherTransport struct{}

func (blockingCypherTransport) CallTool(
	ctx context.Context,
	_ string,
	_ map[string]any,
) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func (blockingCypherTransport) Close() error { return nil }

func TestCypherForwardsCallerCancellationToCommonTransport(t *testing.T) {
	c := &Client{transport: blockingCypherTransport{}}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Cypher(ctx, "RETURN 1", nil, false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cypher error = %v, want caller deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Cypher ignored caller cancellation for %v", elapsed)
	}
}
