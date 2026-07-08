// materialize.go is the docker-cp bridge (D-10): host source dirs (the identity's skills /
// Agent.md / pyscripts) are tar-streamed INTO the box volume via CopyToContainer, and box
// /workspace artifacts are streamed OUT via CopyFromContainer. This replaces the removed ro
// bind-mount (unrepresentable under SBX-02 — host binds have no vector), so skills land at
// the SAME /skills/<name>/... root SnippetSandboxPath renders, by construction. Everything
// goes through the Go SDK tar stream, never a shelled `docker cp` (Pitfall 6 MSYS mangling).

package usersandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/moby/moby/client"
)

// MaterializeIn tar-streams each source's host dir into the box, landing its contents under
// the source's Dest root (e.g. skills at "/skills", so the in-box path equals the one
// SnippetSandboxPath renders). A missing host dir is skipped (nothing to materialize); a
// symlink or non-regular file is rejected (no symlink escape — sandbox-runtime guard). It is
// called from Resolve at BOTH create and resume, and MUST fail Resolve closed on error.
func MaterializeIn(ctx context.Context, cli *client.Client, h BoxHandle, srcs []MaterializeSource) error {
	for _, s := range srcs {
		if strings.TrimSpace(s.HostDir) == "" || strings.TrimSpace(s.Dest) == "" {
			continue
		}
		info, err := os.Stat(s.HostDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("materialize stat %q: %w", s.HostDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("materialize source %q is not a directory", s.HostDir)
		}
		stream, err := tarDir(s.HostDir, s.Dest)
		if err != nil {
			return fmt.Errorf("materialize tar %q: %w", s.HostDir, err)
		}
		// Extract at "/" with dest-rooted entry names; the daemon MkdirAll's the parents, so
		// a deep Dest (e.g. /root/.aura/agents) needs no pre-existing directory in the box.
		if _, err := cli.CopyToContainer(ctx, h.ContainerID, client.CopyToContainerOptions{
			DestinationPath: "/",
			Content:         stream,
		}); err != nil {
			return fmt.Errorf("materialize cp %q -> %q: %w", s.HostDir, s.Dest, err)
		}
	}
	return nil
}

// CopyArtifactsOut returns a tar stream of boxPath (a box /workspace artifact) via
// CopyFromContainer — the seam send_file (37-07) consumes for Telegram sendDocument delivery.
// The caller owns closing the returned reader.
func CopyArtifactsOut(ctx context.Context, cli *client.Client, h BoxHandle, boxPath string) (io.ReadCloser, error) {
	res, err := cli.CopyFromContainer(ctx, h.ContainerID, client.CopyFromContainerOptions{SourcePath: boxPath})
	if err != nil {
		return nil, fmt.Errorf("copy artifacts out %q: %w", boxPath, err)
	}
	return res.Content, nil
}

// CopyArtifactsOut is the DockerBackend method form CopyArtifactOut (router_tools.go) resolves
// structurally so the routed send_file stages a box artifact out via THIS backend's own client.
func (b *DockerBackend) CopyArtifactsOut(ctx context.Context, h BoxHandle, boxPath string) (io.ReadCloser, error) {
	return CopyArtifactsOut(ctx, b.cli, h, boxPath)
}

// CopyFileIn tar-streams content INTO the box at boxPath via CopyToContainer — the host→box
// copy-in the routed fs_write uses (the tar path sidesteps the ARG_MAX + quoting/binary limits an
// in-box `printf`/heredoc write would hit for large content). The daemon MkdirAll's the parents,
// so a deep boxPath (e.g. /workspace/a/b/c.txt) needs no pre-existing directory.
func CopyFileIn(ctx context.Context, cli *client.Client, h BoxHandle, boxPath string, content []byte, mode int64) error {
	stream, err := tarSingleFile(boxPath, content, mode)
	if err != nil {
		return err
	}
	if _, err := cli.CopyToContainer(ctx, h.ContainerID, client.CopyToContainerOptions{
		DestinationPath: "/",
		Content:         stream,
	}); err != nil {
		return fmt.Errorf("copy file in %q: %w", boxPath, err)
	}
	return nil
}

// CopyFileIn is the DockerBackend method form WriteFile (router_tools.go) resolves structurally.
func (b *DockerBackend) CopyFileIn(ctx context.Context, h BoxHandle, boxPath string, content []byte, mode int64) error {
	return CopyFileIn(ctx, b.cli, h, boxPath, content, mode)
}

// tarSingleFile builds a one-entry tar of content rooted at boxPath's "/"-relative name (extracted
// at "/" by CopyToContainer). The path is POSIX-cleaned; a traversal-shaped or empty path is
// rejected (no escape above the box root).
func tarSingleFile(boxPath string, content []byte, mode int64) (io.Reader, error) {
	name := strings.TrimPrefix(pathpkg.Clean("/"+strings.TrimSpace(boxPath)), "/")
	if name == "" || name == ".." || strings.HasPrefix(name, "../") {
		return nil, fmt.Errorf("invalid box path %q", boxPath)
	}
	if mode == 0 {
		mode = 0o644
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

// tarDir builds an in-memory tar of hostDir's tree with every entry rooted at dest (leading/
// trailing slashes trimmed to a relative prefix). Symlinks and other non-regular files are
// rejected rather than followed, closing the symlink-escape vector.
func tarDir(hostDir, dest string) (io.Reader, error) {
	destPrefix := strings.Trim(strings.TrimSpace(dest), "/")
	root := filepath.Clean(hostDir)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to materialize symlink %q (sandbox-runtime symlink guard)", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the root itself is not emitted; entries are rooted at destPrefix
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to materialize non-regular file %q", path)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = destPrefix + "/" + filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := os.Open(path) //nolint:gosec // G304: path is a materialize source under a fixed per-identity host root (skills/Agent.md/pyscripts), produced by the backend's WalkDir — not user input.
		if err != nil {
			return err
		}
		_, cerr := io.Copy(tw, f)
		_ = f.Close()
		return cerr
	})
	if err != nil {
		_ = tw.Close()
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}
