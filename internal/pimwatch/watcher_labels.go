package pimwatch

import (
	"context"
	"log/slog"
)

// seedDescriptions gives the labels an identity had before descriptions existed the default ones,
// once. It runs before the Book is read, so the tick that claims it already sees them. A failure
// leaves the labels as they are and logs no text.
func (w *Watcher) seedDescriptions(ctx context.Context, identityID string) {
	defaults := append(DefaultLabels(LangIT), DefaultLabels(LangEN)...)
	if _, err := w.Store.SeedLabelDescriptions(ctx, identityID, defaults); err != nil {
		slog.Warn("mail watch: the default label descriptions were not seeded", "error", err)
	}
}
