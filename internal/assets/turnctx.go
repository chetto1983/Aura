package assets

// The attachment ids of the turn being served, carried to the writer that records them.
//
// The HTTP layer knows them (the run envelope lists them beside the message) and validates
// them; the runner is what appends the user turn and therefore what can persist the link.
// Nothing between the two has a reason to know, so this rides the context the same way the
// content projection already does rather than widening five signatures on its way through.
//
// It carries EVERY attachment, not the media subset the model's content projection takes.
// A PDF reaches the model as document scope rather than as an image part, but it was still
// sent with this turn, and a link that dropped it would lose the file from the conversation
// on reload exactly as the positional fold did before migration 0116.

import "context"

type turnAttachmentsKey struct{}

// WithTurnAttachments records the ids for the turn about to be appended. An empty list
// stores nothing, so a turn sent with no attachments leaves the column NULL.
func WithTurnAttachments(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	// Copied: the caller's slice is request-scoped and the context outlives the loop that
	// built it.
	held := make([]string, len(ids))
	copy(held, ids)
	return context.WithValue(ctx, turnAttachmentsKey{}, held)
}

// TurnAttachments reports the ids recorded for this turn, or nil when the request carried
// none. nil is the honest answer for every non-HTTP caller — a scheduled delivery, a
// Telegram turn, a delegation — none of which attach assets through this path.
func TurnAttachments(ctx context.Context) []string {
	ids, _ := ctx.Value(turnAttachmentsKey{}).([]string)
	return ids
}
