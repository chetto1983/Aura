package main

import (
	"context"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// emptyProjectionSink is the boot tests' projection double: it accepts every write and
// projects nothing. It lives in its own file because chat_boot_test.go reached the 600-LOC
// cap when the sink grew its watermark read, and the sink is a fixture rather than a test.
type emptyProjectionSink struct{}

func (emptyProjectionSink) ApplyConversationProjection(context.Context, arcadedb.ConversationProjection) error {
	return nil
}
func (emptyProjectionSink) DeleteConversationProjection(context.Context, string, string) error {
	return nil
}
func (emptyProjectionSink) DeleteIdentityConversationProjections(context.Context, string) error {
	return nil
}
func (emptyProjectionSink) PruneConversationProjections(context.Context, string, []string) error {
	return nil
}

// ProjectedThroughSeq answers 0, and 0 is the only honest answer here: this double holds
// no conversation, so the context ladder must claim nothing is recoverable from it.
func (emptyProjectionSink) ProjectedThroughSeq(context.Context, string, string) (int, error) {
	return 0, nil
}
