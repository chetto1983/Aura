package swarm

import (
	"context"
	"errors"
	"testing"
)

// delegation_artifact_test.go is the daemon-free coverage for archiveReport
// itself (51-11) -- isolated from DeliverReport's own ordering/degrade tests
// in delegation_delivery_test.go, which exercise the same seam one layer up.

func TestArchiveReportNilArchiverReturnsEmptyName(t *testing.T) {
	if got := archiveReport(context.Background(), nil, "id-1", "conv-1", "w1", "markdown"); got != "" {
		t.Fatalf("archiveReport(nil archiver) = %q, want empty", got)
	}
}

func TestArchiveReportSuccessReturnsChildIDDotMD(t *testing.T) {
	var gotFilename, gotMarkdown string
	archiver := archiverFunc(func(_ context.Context, identityID, conversationID, deliveryKey, filename, markdown string) (string, error) {
		if identityID != "id-1" || conversationID != "conv-1" {
			t.Fatalf("ArchiveReport called with (%q, %q), want (id-1, conv-1)", identityID, conversationID)
		}
		if deliveryKey != "" {
			t.Fatalf("legacy archive delivery key = %q, want empty", deliveryKey)
		}
		gotFilename, gotMarkdown = filename, markdown
		return "asset-123", nil
	})
	got := archiveReport(context.Background(), archiver, "id-1", "conv-1", "w1-abc", "the report body")
	if got != "w1-abc.md" {
		t.Fatalf("archiveReport = %q, want %q (child id + .md suffix)", got, "w1-abc.md")
	}
	if gotFilename != "w1-abc.md" || gotMarkdown != "the report body" {
		t.Fatalf("ArchiveReport called with (%q, %q), want (w1-abc.md, the report body)", gotFilename, gotMarkdown)
	}
}

func TestArchiveReportErrorDegradesToEmptyName(t *testing.T) {
	archiver := archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		return "", errors.New("garage unreachable")
	})
	got := archiveReport(context.Background(), archiver, "id-1", "conv-1", "w1", "markdown")
	if got != "" {
		t.Fatalf("archiveReport on an ArchiveReport error = %q, want empty (never fails the caller)", got)
	}
}
