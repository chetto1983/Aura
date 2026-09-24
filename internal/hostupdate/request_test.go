package hostupdate

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestWriteRequestLeavesOnlyTheRequestFileBehind(t *testing.T) {
	dir := t.TempDir()
	req := Request{
		ID:     "0123456789abcdef0123456789abcdef",
		Action: ActionDefer,
		Until:  time.Unix(1790320200, 0),
		By:     "448ddbe1-96ea-405d-8219-4a3d52a425c0",
	}
	if err := WriteRequest(dir, req); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != RequestFile {
		t.Fatalf("dir holds %v, want only %s (no temporary file left)", entries, RequestFile)
	}
	body, err := os.ReadFile(filepath.Join(dir, RequestFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "id=0123456789abcdef0123456789abcdef\naction=defer\nuntil=1790320200\nby=448ddbe1-96ea-405d-8219-4a3d52a425c0\n"
	if string(body) != want {
		t.Fatalf("request body\n got %q\nwant %q", body, want)
	}
}

func TestWriteRequestThenReadRequestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	req := Request{ID: "fedcba9876543210fedcba9876543210", Action: ActionApply, By: "448ddbe1-96ea-405d-8219-4a3d52a425c0"}
	if err := WriteRequest(dir, req); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadRequest(dir)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got != req {
		t.Fatalf("got %+v, want %+v", got, req)
	}
}

func TestReadRequestWithoutAFileIsNotAnError(t *testing.T) {
	if _, ok, err := ReadRequest(t.TempDir()); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestWriteRequestRefusesWhatTheUpdaterWouldReject(t *testing.T) {
	good := Request{ID: "0123456789abcdef0123456789abcdef", Action: ActionApply, By: "448ddbe1-96ea-405d-8219-4a3d52a425c0"}
	for name, mutate := range map[string]func(*Request){
		"unknown action":         func(r *Request) { r.Action = "reboot" },
		"short id":               func(r *Request) { r.ID = "abc" },
		"id with newline":        func(r *Request) { r.ID = "0123456789abcdef\naction=apply" },
		"by with junk":           func(r *Request) { r.By = "root\nid=x" },
		"defer without an until": func(r *Request) { r.Action = ActionDefer },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			req := good
			mutate(&req)
			if err := WriteRequest(dir, req); err == nil {
				t.Fatalf("WriteRequest accepted %+v", req)
			}
			if _, err := os.Stat(filepath.Join(dir, RequestFile)); !os.IsNotExist(err) {
				t.Fatalf("a refused request still reached the updater: %v", err)
			}
		})
	}
}

func TestNewRequestIDIsHexAndUnique(t *testing.T) {
	a, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(a) || a == b {
		t.Fatalf("ids %q %q", a, b)
	}
}
