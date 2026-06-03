package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestCodeSandboxAsset(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
		wantErr            bool
	}{
		{"linux", "amd64", "code-sandbox-mcp-linux-amd64", false},
		{"darwin", "arm64", "code-sandbox-mcp-darwin-arm64", false},
		{"windows", "amd64", "code-sandbox-mcp-windows-amd64.exe", false},
		{"windows", "arm64", "code-sandbox-mcp-windows-arm64.exe", false},
		{"plan9", "amd64", "", true},
		{"linux", "mips", "", true},
	}
	for _, tc := range cases {
		got, err := codeSandboxAsset(tc.goos, tc.goarch)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s/%s: want error", tc.goos, tc.goarch)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s/%s = (%q,%v), want %q", tc.goos, tc.goarch, got, err, tc.want)
		}
	}
}

func TestEnsureBinary_DownloadsThenCaches(t *testing.T) {
	const body = "#!/fake-binary\x00\x01payload"
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	dest, err := ensureBinary(context.Background(), srv.Client(), cacheDir, "code-sandbox-mcp-vtest", srv.URL)
	if err != nil {
		t.Fatalf("ensureBinary: %v", err)
	}
	if dest != filepath.Join(cacheDir, "code-sandbox-mcp-vtest") {
		t.Fatalf("dest = %q", dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != body {
		t.Fatalf("downloaded content = %q (err %v), want %q", got, err, body)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(dest)
		if fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("binary not executable: mode %v", fi.Mode())
		}
	}
	// No .partial left behind.
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatalf(".partial should be gone, stat err=%v", err)
	}
	// Second call is a cache hit — server is NOT contacted again.
	if _, err := ensureBinary(context.Background(), srv.Client(), cacheDir, "code-sandbox-mcp-vtest", srv.URL); err != nil {
		t.Fatalf("ensureBinary (cached): %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("server hit %d times, want 1 (second call must be cached)", n)
	}
}

func TestEnsureBinary_HTTPErrorNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	_, err := ensureBinary(context.Background(), srv.Client(), cacheDir, "x", srv.URL)
	if err == nil {
		t.Fatal("want error on HTTP 404")
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "x")); !os.IsNotExist(statErr) {
		t.Fatal("no binary should be written on a failed download")
	}
}

func TestCodeSandboxDownloadURL(t *testing.T) {
	got := codeSandboxDownloadURL("v0.0.30", "code-sandbox-mcp-linux-amd64")
	want := "https://github.com/Automata-Labs-team/code-sandbox-mcp/releases/download/v0.0.30/code-sandbox-mcp-linux-amd64"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}
