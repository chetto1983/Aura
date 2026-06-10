package mcptools

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestMount_CollisionHash_RespectsCap covers WR-02: when two distinct raw tool names
// sanitize to the SAME namespaced base that already sits near the 64-byte cap, the
// collision-hash append must re-truncate before adding the 13-byte suffix. A bare +=
// would yield a 76-byte name; both disambiguated names must stay <= maxToolNameLen,
// distinct, and retrievable.
func TestMount_CollisionHash_RespectsCap(t *testing.T) {
	reg := tools.NewRegistry()
	// body 57 + delimiter "srv__" (5) = 62; +1 sanitized trailing char = 63-byte base.
	// "a..a/" and "a..a." both sanitize to the identical 63-byte "srv__aa..a_".
	body := strings.Repeat("a", 57)
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: body + "/", Description: "first"},
		{Name: body + ".", Description: "second"},
	}}
	names, err := Mount(context.Background(), reg, "srv", srv)
	if err != nil {
		t.Fatalf("Mount with near-cap sanitize-collision must disambiguate, got %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("both tools must register, got %v", names)
	}
	if names[0] == names[1] {
		t.Fatalf("disambiguation failed: both names equal %q", names[0])
	}
	for _, n := range names {
		if len(n) > maxToolNameLen {
			t.Errorf("disambiguated name exceeds the cap: %q len=%d (WR-02)", n, len(n))
		}
		if _, ok := reg.Get(n); !ok {
			t.Errorf("registered name %q not retrievable", n)
		}
	}
}
