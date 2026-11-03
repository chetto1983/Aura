package mcptools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestIdentitySessionReconnectKeepsItsStartupBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		initBudget time.Duration
		wantBudget time.Duration
	}{
		{name: "box", initBudget: 120 * time.Second, wantBudget: 120 * time.Second},
		{name: "http", wantBudget: 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &scriptedOpen{t: t, build: readOnlyLookupServer}
			parent := NewMountedServer("fixture", nil)
			var reconnectBudget time.Duration
			connect := func(pctx, hctx context.Context, opts mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
				if fixture.dialCount() > 0 {
					deadline, ok := hctx.Deadline()
					if !ok {
						return nil, fmt.Errorf("reconnect handshake has no deadline")
					}
					reconnectBudget = time.Until(deadline)
					if reconnectBudget < tc.wantBudget-time.Second || reconnectBudget > tc.wantBudget {
						return nil, fmt.Errorf("server needs its configured startup budget: got %v, want %v", reconnectBudget, tc.wantBudget)
					}
				}
				return fixture.open(pctx, hctx, opts)
			}
			pool := newIdentitySessionPool(parent, connect, t.Context())
			pool.firstStart = tc.initBudget
			parent.identityPool = pool
			t.Cleanup(func() { _ = parent.Close() })
			ctx := identityctx.WithIdentityID(t.Context(), "owner")
			_, advertised, err := pool.openInitial(ctx)
			if err != nil {
				t.Fatalf("initial session: %v", err)
			}
			parent.trackBridgedTools(bridgeTools("fixture", parent, advertised, time.Second))
			child, err := pool.server(ctx)
			if err != nil {
				t.Fatal(err)
			}
			killLiveSession(t, child)
			waitFor(t, child.isDead, "session death")

			text, err := parent.CallToolText(ctx, "lookup", map[string]any{"query": "cached restart"})
			if err != nil || !strings.HasPrefix(text, "lookup:") {
				t.Fatalf("tool after reconnect = %q, %v (budget %v)", text, err, reconnectBudget)
			}
			if fixture.dialCount() != 2 {
				t.Fatalf("opens = %d, want initial session and one reconnect", fixture.dialCount())
			}
		})
	}
}
