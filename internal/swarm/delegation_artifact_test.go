package swarm

import (
	"context"
	"errors"
	"testing"
)

// delegation_artifact_test.go is the daemon-free coverage for archivePreparedReport
// itself (51-11): the terminal path's archive step, one layer below deliverPending.

func TestArchivePreparedReportNilArchiverIsAnError(t *testing.T) {
	if got, err := archivePreparedReport(context.Background(), nil, "id-1", "conv-1", "dk-1", "w1", "markdown"); err == nil || got != "" {
		t.Fatalf("archivePreparedReport(nil archiver) = (%q, %v), want a retryable error", got, err)
	}
}

func TestArchivePreparedReportSuccessReturnsChildIDDotMD(t *testing.T) {
	var gotKey, gotFilename, gotMarkdown string
	archiver := archiverFunc(func(_ context.Context, identityID, conversationID, deliveryKey, filename, markdown string) (string, error) {
		if identityID != "id-1" || conversationID != "conv-1" {
			t.Fatalf("ArchiveReport called with (%q, %q), want (id-1, conv-1)", identityID, conversationID)
		}
		gotKey, gotFilename, gotMarkdown = deliveryKey, filename, markdown
		return "asset-123", nil
	})
	got, err := archivePreparedReport(context.Background(), archiver, "id-1", "conv-1", "dk-1", "w1-abc", "the report body")
	if err != nil || got != "w1-abc.md" {
		t.Fatalf("archivePreparedReport = (%q, %v), want (%q, nil): child id + .md suffix", got, err, "w1-abc.md")
	}
	if gotKey != "dk-1" || gotFilename != "w1-abc.md" || gotMarkdown != "the report body" {
		t.Fatalf("ArchiveReport called with (%q, %q, %q), want (dk-1, w1-abc.md, the report body)", gotKey, gotFilename, gotMarkdown)
	}
}

func TestArchivePreparedReportErrorKeepsTheRowRetryable(t *testing.T) {
	archiver := archiverFunc(func(context.Context, string, string, string, string, string) (string, error) {
		return "", errors.New("garage unreachable")
	})
	if got, err := archivePreparedReport(context.Background(), archiver, "id-1", "conv-1", "dk-1", "w1", "markdown"); err == nil || got != "" {
		t.Fatalf("archivePreparedReport on an ArchiveReport error = (%q, %v), want an error", got, err)
	}
}
