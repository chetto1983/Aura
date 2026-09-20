package cloudflaresupervisor

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "tunnel" {
		fakeProcess()
		return
	}
	goleak.VerifyTestMain(m)
}

func fakeProcess() {
	if len(os.Args) != 10 || os.Args[2] != "--no-autoupdate" || os.Args[3] != "--protocol" || os.Args[4] != "auto" || os.Args[5] != "--metrics" || os.Args[7] != "run" || os.Args[8] != "--token-file" {
		os.Exit(90)
	}
	b, err := os.ReadFile(os.Args[9])
	if err != nil {
		os.Exit(91)
	}
	if strings.Contains(strings.Join(os.Environ(), " "), "SYNTHETIC_PARENT_SECRET") {
		os.Exit(92)
	}
	// Deliberately emit sensitive-looking output; the parent must discard it.
	fmt.Fprintln(os.Stderr, "synthetic-secret", os.Args[9])
	if string(b) == "exit" {
		os.Exit(2)
	}
	if string(b) != "ready" {
		os.Exit(93)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	if err := http.ListenAndServe(os.Args[6], mux); err != nil {
		os.Exit(94)
	}
}

func TestProcessLauncherSnapshotsTokenAndReapsChild(t *testing.T) {
	t.Setenv("SYNTHETIC_PARENT_SECRET", "must-not-inherit")
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child, err := (ProcessLauncher{Binary: binary, TempDir: dir, StopTimeout: 20 * time.Millisecond}).Start(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.Stop)
	if err := os.WriteFile(path, []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !child.Ready(context.Background()) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !child.Ready(context.Background()) {
		t.Fatal("synthetic child never became ready")
	}
	child.Stop()
	child.Stop()
	if !child.Exited() || child.Ready(context.Background()) {
		t.Fatal("child survived stop")
	}
	files, err := filepath.Glob(filepath.Join(dir, "aura-connector-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("private snapshot survived stop")
	}
}

func TestProcessLauncherFailureCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	launcher := ProcessLauncher{Binary: filepath.Join(dir, "absent"), TempDir: dir}
	for _, token := range []string{"", strings.Repeat("x", 64*1024+1), "ready"} {
		if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := launcher.Start(path); err == nil {
			t.Fatal("invalid process launch succeeded")
		}
	}
	if _, err := launcher.Start(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing token accepted")
	}
	launcher.TempDir = filepath.Join(dir, "absent")
	if _, err := launcher.Start(path); err == nil {
		t.Fatal("missing snapshot directory accepted")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "aura-connector-*"))
	if len(files) != 0 {
		t.Fatal("failed launch left a token copy")
	}
}

func TestProcessExitIsObserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("exit"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	child, err := (ProcessLauncher{Binary: binary, TempDir: dir}).Start(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.Stop)
	deadline := time.Now().Add(5 * time.Second)
	for !child.Exited() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !child.Exited() {
		t.Fatal("exit was not observed")
	}
}
