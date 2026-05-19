package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestEnsurePiperVoice_CacheHit(t *testing.T) {
	onnxBody := []byte("fake-piper-onnx-cached")
	cfgBody := []byte("fake-piper-cfg-cached")
	onnxSHA := sha256Hex(onnxBody)
	cfgSHA := sha256Hex(cfgBody)

	dir := t.TempDir()
	piperDir := filepath.Join(dir, "piper")
	if err := os.MkdirAll(piperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piperDir, PiperVoiceFilename), onnxBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piperDir, PiperVoiceConfigFilename), cfgBody, 0o644); err != nil {
		t.Fatal(err)
	}

	hits := atomic.Int64{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "should not be called on cache-hit", http.StatusInternalServerError)
	}))
	defer hs.Close()

	t.Setenv(envPiperVoiceURL, hs.URL+"/onnx")
	t.Setenv(envPiperVoiceSHA256, onnxSHA)
	t.Setenv(envPiperVoiceCfgURL, hs.URL+"/cfg")
	t.Setenv(envPiperVoiceCfgSHA256, cfgSHA)

	if err := EnsurePiperVoice(context.Background(), dir, nil); err != nil {
		t.Fatalf("EnsurePiperVoice: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server hit %d times on cache-hit path; expected 0", hits.Load())
	}
}

func TestEnsurePiperVoice_CorruptRedownload(t *testing.T) {
	onnxBody := []byte("fake-piper-onnx-correct")
	cfgBody := []byte("fake-piper-cfg-correct")
	onnxSHA := sha256Hex(onnxBody)
	cfgSHA := sha256Hex(cfgBody)

	dir := t.TempDir()
	piperDir := filepath.Join(dir, "piper")
	if err := os.MkdirAll(piperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Corrupt the ONNX file; JSON is absent so it will also be downloaded.
	if err := os.WriteFile(filepath.Join(piperDir, PiperVoiceFilename), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onnx":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(onnxBody)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cfgBody)
		}
	}))
	defer hs.Close()

	t.Setenv(envPiperVoiceURL, hs.URL+"/onnx")
	t.Setenv(envPiperVoiceSHA256, onnxSHA)
	t.Setenv(envPiperVoiceCfgURL, hs.URL+"/cfg")
	t.Setenv(envPiperVoiceCfgSHA256, cfgSHA)

	if err := EnsurePiperVoice(context.Background(), dir, nil); err != nil {
		t.Fatalf("EnsurePiperVoice: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(piperDir, PiperVoiceFilename))
	if err != nil {
		t.Fatalf("read onnx dest: %v", err)
	}
	if string(got) != string(onnxBody) {
		t.Fatalf("ONNX not re-downloaded: got %q, want %q", got, onnxBody)
	}
	gotCfg, err := os.ReadFile(filepath.Join(piperDir, PiperVoiceConfigFilename))
	if err != nil {
		t.Fatalf("read cfg dest: %v", err)
	}
	if string(gotCfg) != string(cfgBody) {
		t.Fatalf("config not downloaded: got %q, want %q", gotCfg, cfgBody)
	}
}

func TestEnsurePiperVoice_EnvOverride(t *testing.T) {
	onnxBody := []byte("fake-piper-onnx-env")
	cfgBody := []byte("fake-piper-cfg-env")
	onnxSHA := sha256Hex(onnxBody)
	cfgSHA := sha256Hex(cfgBody)

	dir := t.TempDir()

	onnxHits := atomic.Int64{}
	cfgHits := atomic.Int64{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onnx":
			onnxHits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(onnxBody)
		default:
			cfgHits.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cfgBody)
		}
	}))
	defer hs.Close()

	// Override all 4 env vars; default constants must NOT be used.
	t.Setenv(envPiperVoiceURL, hs.URL+"/onnx")
	t.Setenv(envPiperVoiceSHA256, onnxSHA)
	t.Setenv(envPiperVoiceCfgURL, hs.URL+"/cfg")
	t.Setenv(envPiperVoiceCfgSHA256, cfgSHA)

	if err := EnsurePiperVoice(context.Background(), dir, nil); err != nil {
		t.Fatalf("EnsurePiperVoice: %v", err)
	}
	if onnxHits.Load() == 0 {
		t.Fatal("ONNX env URL override not honoured: expected ≥1 hit")
	}
	if cfgHits.Load() == 0 {
		t.Fatal("config env URL override not honoured: expected ≥1 hit")
	}
}
