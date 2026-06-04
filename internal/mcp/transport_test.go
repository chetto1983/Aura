package mcp

import (
	"context"
	"testing"
)

func TestStdioClientImplementsTransportAndPing(t *testing.T) {
	var _ Transport = (*Client)(nil)
	c, cleanup := newTestPair(t)
	defer cleanup()
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
