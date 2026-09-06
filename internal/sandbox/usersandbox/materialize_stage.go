// materialize_stage.go is the STAGING half of the docker-cp bridge: it turns each
// MaterializeSource into one tar on disk, ready for MaterializeIn's extraction pass. It was
// split out of materialize.go on touch — that file owns the mirror PLAN (which dests are
// cleared, in what order, and what an `rm -rf` may be handed), and this one owns the question
// of what a source becomes and what happens when it cannot become anything.
//
// Two properties live here and nowhere else:
//
//   - EVERY tar is built before the first clear (the ordering MaterializeIn depends on), and
//     each is SPOOLED TO A FILE rather than held in memory, so the peak cost of a resume is
//     one tar's buffering instead of the sum of every source's tree. The sum was the shape
//     before, and it grew with the number of skills shared with the reader.
//   - A SHARED source is individually skippable and every other source is not
//     (MaterializeSource.SkipOnFault).

package usersandbox

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// stagedSource is one source's tar, spooled to a file and waiting for the extraction pass.
type stagedSource struct {
	hostDir string
	dest    string
	tar     *os.File
}

// close releases one spooled tar: the open handle and the file behind it. It is safe to call
// twice and on a zero value, because the cleanup paths that reach it are the error paths.
func (s stagedSource) close() {
	if s.tar == nil {
		return
	}
	_ = s.tar.Close()
	_ = os.Remove(s.tar.Name())
}

// stagedSources is the whole staging plan. Its close is what makes the spool safe: MaterializeIn
// defers it, so the temp files are removed on the success path, on a clear failure, on a
// CopyToContainer failure, and on a context cancellation alike.
type stagedSources []stagedSource

func (ss stagedSources) close() {
	for _, s := range ss {
		s.close()
	}
}

// errSourceGone marks a host dir that no longer exists. It is not a fault: that is an identity
// who has written no skill of their own, or a share that has just been revoked, and the mirror
// answers both by clearing the dest and landing nothing.
var errSourceGone = errors.New("materialize: source host dir is gone")

// tarSources spools the tar of every source that still exists, in list order.
//
// A source that FAULTS — a path that exists and is not a directory, a symlink or non-regular
// file inside the tree, an unreadable entry — fails the whole materialization, EXCEPT when it
// is marked SkipOnFault, which the composition root sets for shared sources only. See
// MaterializeSource.SkipOnFault for why the two halves of that rule point in opposite
// directions.
//
// A failure part-way through closes the tars already spooled before returning: the caller gets
// an error and no files, never an error and a directory quietly filling up.
func tarSources(srcs []MaterializeSource, spoolDir string) (stagedSources, error) {
	if err := ensureSpoolDir(spoolDir); err != nil {
		return nil, err
	}
	out := make(stagedSources, 0, len(srcs))
	for _, s := range srcs {
		if strings.TrimSpace(s.HostDir) == "" || strings.TrimSpace(s.Dest) == "" {
			continue
		}
		f, err := spoolSource(s, spoolDir)
		switch {
		case errors.Is(err, errSourceGone):
			continue
		case err != nil && s.SkipOnFault:
			// The reason is logged rather than swallowed: a shared skill that never reaches
			// the box has to be findable from the daemon's own output, or the grantee's only
			// symptom is a skill that silently is not there.
			slog.Warn("materialize: skipping a shared source that could not be staged — the rest of the box is unaffected",
				"host_dir", s.HostDir, "dest", s.Dest, "err", err)
			continue
		case err != nil:
			out.close()
			return nil, err
		}
		out = append(out, stagedSource{hostDir: s.HostDir, dest: s.Dest, tar: f})
	}
	return out, nil
}

// ensureSpoolDir creates the spool directory when one is named. An empty spoolDir means
// os.TempDir(), which always exists.
func ensureSpoolDir(spoolDir string) error {
	if strings.TrimSpace(spoolDir) == "" {
		return nil
	}
	if err := os.MkdirAll(spoolDir, 0o750); err != nil {
		return fmt.Errorf("materialize spool dir %q: %w", spoolDir, err)
	}
	return nil
}

// spoolSource validates one source's host path and spools its tree to a tar file, rewound and
// ready to stream. A host dir that is gone yields errSourceGone, which is not a fault.
func spoolSource(s MaterializeSource, spoolDir string) (*os.File, error) {
	info, err := os.Stat(s.HostDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errSourceGone
		}
		return nil, fmt.Errorf("materialize stat %q: %w", s.HostDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("materialize source %q is not a directory", s.HostDir)
	}
	f, err := os.CreateTemp(spoolDir, "aura-materialize-*.tar")
	if err != nil {
		return nil, fmt.Errorf("materialize spool %q: %w", s.HostDir, err)
	}
	if err := writeTarDir(f, s.HostDir, s.Dest); err != nil {
		stagedSource{tar: f}.close()
		return nil, fmt.Errorf("materialize tar %q: %w", s.HostDir, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		stagedSource{tar: f}.close()
		return nil, fmt.Errorf("materialize rewind %q: %w", s.HostDir, err)
	}
	return f, nil
}

// writeTarDir writes a tar of hostDir's tree to w with every entry rooted at dest (leading/
// trailing slashes trimmed to a relative prefix). Symlinks and other non-regular files are
// rejected rather than followed, closing the symlink-escape vector.
func writeTarDir(w io.Writer, hostDir, dest string) error {
	destPrefix := strings.Trim(strings.TrimSpace(dest), "/")
	root := filepath.Clean(hostDir)
	tw := tar.NewWriter(w)
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
		f, err := os.Open(path) //nolint:gosec // G304: path is a materialize source under a fixed host root the resolver supplies, produced by the backend's WalkDir — not user input.
		if err != nil {
			return err
		}
		_, cerr := io.Copy(tw, f)
		_ = f.Close()
		return cerr
	})
	if err != nil {
		_ = tw.Close()
		return err
	}
	return tw.Close()
}
