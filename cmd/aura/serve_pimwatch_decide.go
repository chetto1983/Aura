// serve_pimwatch_decide.go wires the mail watch's decision model: the route the cockpit sets, read
// on every tick, and the client that asks it.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/chetto1983/aura/internal/decide"
	"github.com/chetto1983/aura/internal/pimwatch"
	"github.com/chetto1983/aura/internal/settings"
)

// pimWatchDecider is the watch's decision model. It returns a true nil without a pool or with a
// settings store it cannot open, so the watch keeps its ladder alone.
func pimWatchDecider(chat *chatEnv) pimwatch.Decider {
	if chat.pool == nil || chat.cfg == nil {
		return nil
	}
	store, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("mail watch: the decision model is off, aura.settings cannot be opened", "error", err)
		return nil
	}
	return pimDecider{settings: store, client: decide.Client{HTTP: &http.Client{Timeout: pimwatch.DecideBudget}}}
}

type pimDecider struct {
	settings settings.SecretLister
	client   decide.Client
}

func (d pimDecider) Route(ctx context.Context) (decide.Route, error) {
	return settings.DecideRoute(ctx, d.settings, bootDecideLookup)
}

// Choose logs a failure by its class alone: an OpenRouter error's text carries the response body. A
// call the tick's budget cut or its run cancelled is no failure, and the tick's summary counts it.
func (d pimDecider) Choose(ctx context.Context, route decide.Route, state string, q decide.Question) (decide.Answer, error) {
	answer, err := d.client.Choose(ctx, route, state, q)
	if err != nil && ctx.Err() == nil && !errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("mail watch: the decision model did not answer", "class", decide.ErrorClass(err))
	}
	return answer, err
}

// bootDecideLookup answers the decide wiring's two names from the boot read, taken before the
// overlay, so a deleted AURA_DECIDE_MODEL row falls back to what the deployment named. Every other
// name, the services key's among them, reads the environment.
func bootDecideLookup(key string) (string, bool) {
	switch key {
	case "AURA_DECIDE_BASE_URL":
		return bootDecide().BaseURL, true
	case "AURA_DECIDE_MODEL":
		return bootDecide().Model, true
	default:
		return os.LookupEnv(key)
	}
}
