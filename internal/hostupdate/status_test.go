package hostupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadStatusWithoutAFileMeansTheHostIsNotManaged(t *testing.T) {
	_, managed, err := ReadStatus(t.TempDir())
	if err != nil || managed {
		t.Fatalf("managed=%v err=%v, want an unmanaged host and no error", managed, err)
	}
}

func TestReadStatusParsesWhatTheUpdaterWrites(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, StatusFile, strings.Join([]string{
		"state=pending",
		"running_rev=4de507676c6b234470d59ce57ce9bfbe61233cfe",
		"available_rev=7886200e5000000000000000000000000000abcd",
		"available_built=1790316300",
		"pending_since=1790316600",
		"deadline=1790403000",
		"deferred_until=1790320200",
		"deferred_by=448ddbe1-96ea-405d-8219-4a3d52a425c0",
		"handled_request=0123456789abcdef0123456789abcdef",
		"error=",
		"checked_at=1790316900",
		"a_key_a_newer_updater_added=whatever",
	}, "\n")+"\n")

	got, managed, err := ReadStatus(dir)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	want := Status{
		State:          StatePending,
		RunningRev:     "4de507676c6b234470d59ce57ce9bfbe61233cfe",
		AvailableRev:   "7886200e5000000000000000000000000000abcd",
		AvailableBuilt: time.Unix(1790316300, 0).UTC(),
		PendingSince:   time.Unix(1790316600, 0).UTC(),
		Deadline:       time.Unix(1790403000, 0).UTC(),
		DeferredUntil:  time.Unix(1790320200, 0).UTC(),
		DeferredBy:     "448ddbe1-96ea-405d-8219-4a3d52a425c0",
		HandledRequest: "0123456789abcdef0123456789abcdef",
		CheckedAt:      time.Unix(1790316900, 0).UTC(),
	}
	if got != want {
		t.Fatalf("status mismatch\n got %+v\nwant %+v", got, want)
	}
}

func TestReadStatusLeavesAbsentAndZeroTimesZero(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, StatusFile, "state=current\nrunning_rev=abc1234\ndeferred_until=0\n")
	got, _, err := ReadStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.DeferredUntil.IsZero() || !got.Deadline.IsZero() || got.State != StateCurrent {
		t.Fatalf("got %+v, want current with zero times", got)
	}
}

func TestReadStatusKeepsTheUpdatersErrorLineVerbatim(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, StatusFile, "state=failed\nerror=aura did not become healthy within 600s = timeout\n")
	got, _, err := ReadStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Error != "aura did not become healthy within 600s = timeout" {
		t.Fatalf("error = %q", got.Error)
	}
}

func TestReadStatusRefusesWhatTheUpdaterNeverWrites(t *testing.T) {
	for name, body := range map[string]string{
		"unknown state":      "state=exploded\n",
		"missing state":      "running_rev=abc1234\n",
		"non-numeric time":   "state=pending\npending_since=yesterday\n",
		"revision with junk": "state=pending\navailable_rev=abc;rm -rf /\n",
		"line without equal": "state=pending\ngarbage\n",
		"identity with junk": "state=pending\ndeferred_by=../../etc\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, StatusFile, body)
			if _, _, err := ReadStatus(dir); err == nil {
				t.Fatalf("ReadStatus accepted %q", body)
			}
		})
	}
}
