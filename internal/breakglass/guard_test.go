package breakglass

import (
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

// TestSelectSoleOperator pins the locked D-11 active/deactivated resolution rule:
// exactly one ACTIVE operator wins (even alongside deactivated stragglers); a lone
// deactivated operator is still recoverable; every ambiguous or absent state is
// refused so the caller performs zero writes. Non-'user' kinds are never counted.
func TestSelectSoleOperator(t *testing.T) {
	t.Parallel()

	user := func(id string, deactivated bool) identity.Identity {
		return identity.Identity{ID: id, Name: "op-" + id, Kind: "user", Deactivated: deactivated}
	}
	nonUser := func(id, kind string) identity.Identity {
		return identity.Identity{ID: id, Name: kind + "-" + id, Kind: kind}
	}

	tests := []struct {
		name    string
		ids     []identity.Identity
		wantID  string // empty when an error is expected
		wantErr bool
	}{
		{
			name:    "zero identities",
			ids:     nil,
			wantErr: true,
		},
		{
			name:   "single active operator",
			ids:    []identity.Identity{user("a", false)},
			wantID: "a",
		},
		{
			name:    "two active operators are ambiguous",
			ids:     []identity.Identity{user("a", false), user("b", false)},
			wantErr: true,
		},
		{
			name:   "one active alongside a deactivated straggler",
			ids:    []identity.Identity{user("a", false), user("b", true)},
			wantID: "a",
		},
		{
			name:   "lone deactivated operator is recoverable",
			ids:    []identity.Identity{user("a", true)},
			wantID: "a",
		},
		{
			name:    "two deactivated operators none active",
			ids:     []identity.Identity{user("a", true), user("b", true)},
			wantErr: true,
		},
		{
			name:   "non-user kinds are never counted or selected",
			ids:    []identity.Identity{nonUser("local", "system"), nonUser("c", "channel"), nonUser("s", "service"), user("a", false)},
			wantID: "a",
		},
		{
			name:    "only non-user kinds present",
			ids:     []identity.Identity{nonUser("local", "system"), nonUser("c", "channel")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := selectSoleOperator(tt.ids)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("selectSoleOperator(%+v) = %+v, nil; want an error", tt.ids, got)
				}
				if got != (identity.Identity{}) {
					t.Fatalf("selectSoleOperator returned %+v on error; want the zero Identity", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectSoleOperator(%+v) unexpected error: %v", tt.ids, err)
			}
			if got.ID != tt.wantID {
				t.Fatalf("selectSoleOperator selected ID %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}
