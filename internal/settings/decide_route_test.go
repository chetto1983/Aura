package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
)

const localDecideBase = "http://aura-llama-decide:8085"

func decideRoute(t *testing.T, store fakeRouteStore, environment map[string]string) (route routeView) {
	t.Helper()
	got, err := DecideRoute(context.Background(), store, env(environment))
	if err != nil {
		t.Fatalf("DecideRoute: %v", err)
	}
	return routeView{BaseURL: got.BaseURL, Model: got.Model, Off: got.Off(), Hosted: got.Hosted()}
}

type routeView struct {
	BaseURL, Model string
	Off, Hosted    bool
}

func decideRow(value string) fakeRouteStore {
	return fakeRouteStore{rows: []sqlc.AuraSettings{{Key: "AURA_DECIDE_MODEL", Value: value}}}
}

func TestDecideRouteReadsTheModelLiveAndTheAddressFromTheEnvironment(t *testing.T) {
	environment := map[string]string{"AURA_DECIDE_BASE_URL": localDecideBase}

	if got := decideRoute(t, decideRow("liquid/d1.1"), environment); !got.Hosted || got.Model != "liquid/d1.1" || got.BaseURL != llm.DefaultBaseURL {
		t.Fatalf("row liquid/d1.1 = %+v, want the hosted route on OpenRouter", got)
	}
	if got := decideRoute(t, decideRow("off"), environment); !got.Off {
		t.Fatalf("row off = %+v, want the route off even with a sidecar installed", got)
	}
	if got := decideRoute(t, decideRow(""), environment); got.Hosted || got.BaseURL != localDecideBase || got.Model != "" {
		t.Fatalf("empty row = %+v, want the local sidecar", got)
	}
	if got := decideRoute(t, fakeRouteStore{}, environment); got.BaseURL != localDecideBase || got.Hosted {
		t.Fatalf("no row = %+v, want the environment's answer: local", got)
	}
	if got := decideRoute(t, decideRow(""), nil); !got.Off {
		t.Fatalf("empty row and no sidecar = %+v, want off", got)
	}

	hijack := fakeRouteStore{rows: []sqlc.AuraSettings{
		{Key: "AURA_DECIDE_MODEL", Value: ""}, {Key: "AURA_DECIDE_BASE_URL", Value: "http://attacker.example:1"},
	}}
	if got := decideRoute(t, hijack, environment); got.BaseURL != localDecideBase {
		t.Fatalf("a row named AURA_DECIDE_BASE_URL moved the address to %q: the address is the installer's", got.BaseURL)
	}

	row := decideRow("liquid/d1.1")
	first := decideRoute(t, row, environment)
	row.rows[0].Value = "off"
	if second := decideRoute(t, row, environment); !first.Hosted || !second.Off {
		t.Fatalf("first %+v second %+v, want the change seen with no restart", first, second)
	}
}

func TestDecideRouteIsAnErrorWhenTheRowsCannotBeRead(t *testing.T) {
	_, err := DecideRoute(context.Background(), fakeRouteStore{listErr: errors.New("db down")}, env(nil))
	if err == nil {
		t.Fatal("DecideRoute read no rows and said nothing")
	}
}

func TestTheHostedRouteSpendsTheServicesKey(t *testing.T) {
	store := decideRow("liquid/d1.1")
	store.secrets = map[string]string{ServicesKeySetting: "services-key", EmbedEndpointKeySetting: "other-key"}
	route, err := DecideRoute(context.Background(), store, env(nil))
	if err != nil {
		t.Fatalf("DecideRoute: %v", err)
	}
	key, err := route.Key(context.Background())
	if err != nil || key != "services-key" {
		t.Fatalf("key = %q, %v, want the services key", key, err)
	}

	local, err := DecideRoute(context.Background(), decideRow(""), env(map[string]string{"AURA_DECIDE_BASE_URL": localDecideBase}))
	if err != nil {
		t.Fatalf("DecideRoute: %v", err)
	}
	if local.Key != nil {
		t.Fatal("the local route carries a key reader: it sends no key")
	}
}
