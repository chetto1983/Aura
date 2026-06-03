// Binary auto-provisioning for built-in MCP servers. Aura ships zero-setup: if a
// built-in server's binary (code-sandbox-mcp) is not already cached, EnsureCode-
// SandboxBinary downloads the pinned release asset for this OS/arch into the cache
// dir and returns its path — the operator never installs anything by hand.
//
// The version is PINNED (not "latest") for reproducibility: the same Aura build
// always provisions the same sandbox server, and the binary is spawned with
// -no-update so it never mutates itself underneath us.
package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	// codeSandboxRepo / codeSandboxVersion pin the provisioned server. v0.0.30 is
	// the release probed working against Docker Desktop on 2026-06-03.
	codeSandboxRepo    = "Automata-Labs-team/code-sandbox-mcp"
	codeSandboxVersion = "v0.0.30"
)

// codeSandboxAsset maps a GOOS/GOARCH to the release asset name. The project
// publishes linux/darwin/windows × amd64/arm64; anything else is unsupported.
func codeSandboxAsset(goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return "", fmt.Errorf("code-sandbox-mcp: unsupported OS %q", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("code-sandbox-mcp: unsupported arch %q", goarch)
	}
	name := fmt.Sprintf("code-sandbox-mcp-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

// codeSandboxDownloadURL builds the GitHub release-asset URL for a pinned version.
func codeSandboxDownloadURL(version, asset string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", codeSandboxRepo, version, asset)
}

// DefaultCacheDir is ~/.aura/bin — where auto-provisioned MCP binaries are cached
// (alongside ~/.aura/agents, ~/.aura/pyscripts).
func DefaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".aura", "bin"), nil
}

// EnsureCodeSandboxBinary returns the path to a cached code-sandbox-mcp binary for
// this OS/arch, downloading the pinned release into cacheDir if absent. An empty
// cacheDir resolves to DefaultCacheDir. The returned path is safe to spawn.
func EnsureCodeSandboxBinary(ctx context.Context, cacheDir string) (string, error) {
	if cacheDir == "" {
		d, err := DefaultCacheDir()
		if err != nil {
			return "", err
		}
		cacheDir = d
	}
	asset, err := codeSandboxAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	// Version-stamp the cached name so a version bump re-provisions cleanly.
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	destName := fmt.Sprintf("code-sandbox-mcp-%s%s", codeSandboxVersion, ext)
	url := codeSandboxDownloadURL(codeSandboxVersion, asset)
	return ensureBinary(ctx, provisionClient(), cacheDir, destName, url)
}

// provisionClient is the HTTP client for binary provisioning. It uses NO proxy
// (Proxy: nil): the ambient HTTP_PROXY/HTTPS_PROXY in Aura's environment is the
// sandbox container-egress proxy, not an internet proxy — routing this GitHub
// download through it would hang. A bounded timeout keeps a stalled download from
// blocking agent boot (the caller degrades clean on error).
func provisionClient() *http.Client {
	return &http.Client{
		Timeout:   90 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
}

// ensureBinary returns cacheDir/destName, downloading it from url if it is not
// already present and non-empty. The download lands on a .partial temp file that
// is chmod'd executable then atomically renamed, so a crashed/partial download
// never leaves a half-written binary at the final path. Split from EnsureCode-
// SandboxBinary (which wires the real GitHub URL + runtime OS/arch) so the
// download/cache logic is unit-testable against an httptest server.
func ensureBinary(ctx context.Context, client *http.Client, cacheDir, destName, url string) (string, error) {
	dest := filepath.Join(cacheDir, destName)
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return dest, nil // already provisioned
	}
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return "", fmt.Errorf("create cache dir %s: %w", cacheDir, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("code-sandbox-mcp download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("code-sandbox-mcp download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("code-sandbox-mcp download %s: HTTP %d", url, resp.StatusCode)
	}

	tmp := dest + ".partial"
	// G302/G304: dest is built from an operator-controlled cache dir + a pinned
	// asset name (not untrusted input), and the file is an executable binary we
	// will spawn, so it MUST carry the 0o755 exec bit.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close %s: %w", tmp, err)
	}
	// chmod again post-close (umask may have masked the OpenFile mode) — no-op on Windows.
	_ = os.Chmod(tmp, 0o755) //nolint:gosec // G302: executable binary must be 0o755
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("install %s: %w", dest, err)
	}
	return dest, nil
}
