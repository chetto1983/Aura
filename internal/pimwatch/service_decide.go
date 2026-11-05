package pimwatch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chetto1983/aura/internal/decide"
)

// ErrNotWatched is a setting asked for an account that has no watch. It reads as a missing row.
var ErrNotWatched = fmt.Errorf("the account is not watched: %w", ErrNotFound)

// RouteReader says where the decision model runs now. It is read on every call, so the person
// switching the route in the cockpit needs no restart.
type RouteReader interface {
	Route(ctx context.Context) (decide.Route, error)
}

// SetCloudDecide records whether the hosted decision model may read an account's mail. The consent
// is the person's: it survives turning the watch off and on, and leaves with the account.
func (s *Service) SetCloudDecide(ctx context.Context, identityID, accountID string, on bool) error {
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil {
		return err
	}
	if _, ok := findWatch(watches, accountID); !ok {
		return ErrNotWatched
	}
	return s.Store.SetCloudDecide(ctx, identityID, accountID, on)
}

// decideRoute names the route for the status: "local", "openrouter", or "" when there is no model
// step, the route is off, or it cannot be read. The reader's error stays out of the log: it can
// carry a response body.
func (s *Service) decideRoute(ctx context.Context) string {
	if s.Decide == nil {
		return ""
	}
	route, err := s.Decide.Route(ctx)
	switch {
	case err != nil:
		slog.Warn("pimwatch: the decision route could not be read")
		return ""
	case route.Off():
		return ""
	case route.Hosted():
		return "openrouter"
	}
	return "local"
}
