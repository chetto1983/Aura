package swarm

import (
	"context"
	"fmt"
)

// transcript_api.go is SWARM-10's read surface over dumpTranscript's (report.go)
// shipped append-only output: ListChildTranscripts discovers which children have a
// transcript for a conversation, ReadTranscript returns the bytes written so far
// (never blocking, never waiting for EOF) plus a monotonic resume offset. Both are
// package-level functions taking runDir explicitly, mirroring dumpTranscript's own
// shape — this file owns reading, report.go owns writing (the package's existing
// four-way concern split: swarm.go/brief.go/swarm_depth.go/report.go).
//
// STUB (RED phase): both return "not implemented" so transcript_api_test.go fails
// for the right reason before the GREEN commit lands the real logic.

// ReadTranscript returns the transcript bytes for (conv, childID) starting at
// fromOffset, plus the offset the next read should resume from.
func ReadTranscript(ctx context.Context, runDir, conv, childID string, fromOffset int64) ([]byte, int64, error) {
	return nil, fromOffset, fmt.Errorf("swarm: ReadTranscript not implemented")
}

// ListChildTranscripts returns the child ids that have a transcript file under
// conv's swarm directory.
func ListChildTranscripts(ctx context.Context, runDir, conv string) ([]string, error) {
	return nil, fmt.Errorf("swarm: ListChildTranscripts not implemented")
}
