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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

// MaterializeIn makes each source's Dest root inside the box MIRROR its host dir: the stale
// dest is cleared, then the host tree is tar-streamed in, landing under Dest (e.g. skills at
// "/skills", so the in-box path equals the one SnippetSandboxPath renders). A missing host
// dir is skipped (nothing to materialize); a symlink or non-regular file is rejected (no
// symlink escape — sandbox-runtime guard). It is called from Resolve at BOTH create and
// resume, and MUST fail Resolve closed on error.
//
// The clear is what makes this a MIRROR rather than a merge, and it is load-bearing twice
// over. A tar extract only ever adds: without it an archived or deleted skill stayed in the
// box forever — the ledger said gone, the sandbox still ran it — and anything the model
// wrote into /skills itself (an `npx skills add` lands a whole agent-layout tree there)
// accumulated invisibly beside the real skills, indistinguishable from them at use time.
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
		if err := clearDest(ctx, cli, h, s.Dest); err != nil {
			return fmt.Errorf("materialize clear %q: %w", s.Dest, err)
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

// clearDest removes dest inside the box so the following extraction mirrors the host rather
// than merging into it. dest comes from the composition root's SourceResolver, never from a
// model or a request, but it is the argument of an `rm -rf` inside the box, so it is checked
// against destTooBroadToClear first: a misconfigured empty or root-ish dest must fail loudly,
// not erase the box.
func clearDest(ctx context.Context, cli *client.Client, h BoxHandle, dest string) error {
	clean := pathpkg.Clean("/" + strings.Trim(strings.TrimSpace(dest), "/"))
	if destTooBroadToClear(clean) {
		return fmt.Errorf("refusing to clear box path %q (too broad)", dest)
	}
	ec, err := cli.ExecCreate(ctx, h.ContainerID, client.ExecCreateOptions{
		Cmd: []string{"/bin/sh", "-c", "rm -rf -- " + clean},
	})
	if err != nil {
		return fmt.Errorf("exec create: %w", err)
	}
	if _, err := cli.ExecStart(ctx, ec.ID, client.ExecStartOptions{}); err != nil {
		return fmt.Errorf("exec start: %w", err)
	}
	// ExecStart is fire-and-forget: poll ExecInspect until the rm exits so the extraction
	// cannot race the removal and lose freshly-copied files.
	for {
		ins, ierr := cli.ExecInspect(ctx, ec.ID, client.ExecInspectOptions{})
		if ierr != nil {
			return fmt.Errorf("exec inspect: %w", ierr)
		}
		if !ins.Running {
			if ins.ExitCode != 0 {
				return fmt.Errorf("rm -rf %q exited %d", clean, ins.ExitCode)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// destTooBroadToClear rejects the paths an `rm -rf` must never be handed. A materialize dest
// is a dedicated Aura-owned tree the backend fully owns ("/skills", "/root/.aura/agents"), so
// the rule is a denylist of roots that belong to someone else: the box root, the standard
// system directories, and the box's own home/workspace/tmp. A config typo then fails loudly
// instead of erasing the box out from under the running agent.
//
// Depth is deliberately NOT the rule — "/skills", the live dest, is one segment deep.
func destTooBroadToClear(clean string) bool {
	switch clean {
	case "", "/", "/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib32", "/lib64",
		"/media", "/mnt", "/opt", "/proc", "/root", "/run", "/sbin", "/srv", "/sys",
		"/tmp", "/usr", "/var", "/workspace":
		return true
	}
	return !strings.HasPrefix(clean, "/") || strings.Contains(clean, "..")
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

// CopyFileInStream tar-streams exactly size bytes from src INTO the box at boxPath while holding
// none of the file in memory: the tar is produced on one end of an io.Pipe and CopyToContainer
// consumes the other. CopyFileIn's []byte form holds the content TWICE (the slice, plus the fully
// buffered tar), which is fine for the model-authored text fs_write caps at 10 MiB and not fine for
// document_open, whose input is an operator-stored document up to AURA_ASSET_MAX_DOCUMENT_BYTES
// (100 MiB) materialized inside a 768 MiB container.
//
// Failure semantics differ from the buffered form and the caller must know it: tar extracts as the
// daemon reads, so a source that dies mid-stream can leave a SHORT file in the box. The caller owns
// removing it (document_open does).
func CopyFileInStream(
	ctx context.Context,
	cli *client.Client,
	h BoxHandle,
	boxPath string,
	size int64,
	src io.Reader,
	mode int64,
) error {
	hdr, err := singleFileHeader(boxPath, size, mode)
	if err != nil {
		return err
	}
	err = pipeTarEntry(hdr, src, func(r io.Reader) error {
		_, cerr := cli.CopyToContainer(ctx, h.ContainerID, client.CopyToContainerOptions{
			DestinationPath: "/",
			Content:         r,
		})
		return cerr
	})
	if err != nil {
		return fmt.Errorf("stream %q into the box: %w", boxPath, err)
	}
	return nil
}

// pipeTarEntry builds the one-entry tar on one end of an io.Pipe and hands the read end to sink
// (CopyToContainer in production). It is split out from CopyFileInStream because the ordering it
// encodes is the whole risk of the streamed path, and it needs a test that no daemon can give:
//
//   - the reader is closed BEFORE the producer's result is read, or a sink that hangs up early
//     leaves the goroutine blocked on a write nobody will ever drain;
//   - the producer's error wins, because when the SOURCE dies mid-stream the sink only ever sees a
//     truncated body and its transport error would hide the cause the operator needs;
//   - EXCEPT io.ErrClosedPipe, which is what the producer reports when the SINK hung up first —
//     there it is pure noise and the sink holds the real reason.
func pipeTarEntry(hdr *tar.Header, src io.Reader, sink func(io.Reader) error) error {
	pr, pw := io.Pipe()
	produced := make(chan error, 1)
	go func() {
		err := writeTarEntry(pw, hdr, src)
		_ = pw.CloseWithError(err)
		produced <- err
	}()
	sinkErr := sink(pr)
	_ = pr.Close()
	if err := <-produced; err != nil && !errors.Is(err, io.ErrClosedPipe) {
		return err
	}
	return sinkErr
}

// CopyFileInStream is the DockerBackend method form WriteFileStream (router_tools.go) resolves
// structurally.
func (b *DockerBackend) CopyFileInStream(
	ctx context.Context, h BoxHandle, boxPath string, size int64, src io.Reader, mode int64,
) error {
	return CopyFileInStream(ctx, b.cli, h, boxPath, size, src, mode)
}

// tarSingleFile builds a one-entry tar of content rooted at boxPath's "/"-relative name (extracted
// at "/" by CopyToContainer).
func tarSingleFile(boxPath string, content []byte, mode int64) (io.Reader, error) {
	hdr, err := singleFileHeader(boxPath, int64(len(content)), mode)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeTarEntry(&buf, hdr, bytes.NewReader(content)); err != nil {
		return nil, err
	}
	return &buf, nil
}

// singleFileHeader is the one tar entry both copy-in forms share: boxPath POSIX-cleaned to a
// "/"-relative name, with a traversal-shaped or empty path rejected (no escape above the box root).
func singleFileHeader(boxPath string, size, mode int64) (*tar.Header, error) {
	name := strings.TrimPrefix(pathpkg.Clean("/"+strings.TrimSpace(boxPath)), "/")
	if name == "" || name == ".." || strings.HasPrefix(name, "../") {
		return nil, fmt.Errorf("invalid box path %q", boxPath)
	}
	if mode == 0 {
		mode = 0o644
	}
	return &tar.Header{Name: name, Mode: mode, Size: size, Typeflag: tar.TypeReg}, nil
}

// writeTarEntry writes one complete tar holding hdr and EXACTLY hdr.Size bytes from src. tar has no
// length-suffixed framing, so the size is declared before a byte is read; a source that delivers a
// different count therefore fails here rather than landing a file whose length disagrees with the
// record it was opened from. A truncated spreadsheet that looks whole is worse than no file at all,
// because the agent computes a confident wrong answer from it.
func writeTarEntry(w io.Writer, hdr *tar.Header, src io.Reader) error {
	tw := tar.NewWriter(w)
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.Copy(tw, src)
	switch {
	case errors.Is(err, tar.ErrWriteTooLong):
		return fmt.Errorf("source is longer than the declared %d bytes", hdr.Size)
	case err != nil:
		return err
	case n != hdr.Size:
		return fmt.Errorf("source delivered %d bytes, not the declared %d", n, hdr.Size)
	}
	return tw.Close()
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
