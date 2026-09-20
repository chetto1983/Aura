package remotetunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// FileProjection is disposable; callers must persist credentials in PostgreSQL first.
type FileProjection struct {
	root     string
	uid, gid int
	mu       sync.Mutex
}

// NewFileProjection assigns credentials to the sidecar's explicit numeric owner.
func NewFileProjection(root string, uid, gid int) *FileProjection {
	return &FileProjection{root: root, uid: uid, gid: gid}
}

// TokenPath is the private filesystem handoff, never a status field.
func (p *FileProjection) TokenPath() string { return filepath.Join(p.root, "token") }

// Apply publishes atomic files and fsyncs their containing directory on Linux.
func (p *FileProjection) Apply(ctx context.Context, state ProjectionState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.Generation < 0 || (state.Enabled && state.Token.Reveal() == "") {
		return errors.New("invalid tunnel projection")
	}
	if err := os.MkdirAll(p.root, 0o750); err != nil {
		return errors.New("create tunnel projection failed")
	}
	info, err := os.Lstat(p.root)
	if err != nil || !info.IsDir() {
		return errors.New("invalid tunnel projection directory")
	}
	if p.gid >= 0 {
		if err := os.Chown(p.root, -1, p.gid); err != nil {
			return errors.New("set projection directory ownership failed")
		}
	}
	if err := os.Chmod(p.root, 0o750); err != nil { // #nosec G302 -- directory traversal for the sidecar group; token itself is 0600.
		return errors.New("set projection directory permissions failed")
	}
	if state.Enabled {
		if err := p.write("token", []byte(state.Token.Reveal()), 0o600); err != nil {
			return err
		}
	}
	// Publish intent last on enable, first on disable, so a missing token cannot
	// turn a disabled projection into a replacement attempt.
	b, err := json.Marshal(state)
	if err != nil {
		return errors.New("encode tunnel projection failed")
	}
	if err := p.write("desired.json", b, 0o644); err != nil {
		return err
	}
	if !state.Enabled {
		if err := os.Remove(p.TokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("remove tunnel projection failed")
		}
	}
	return p.syncDir()
}

func (p *FileProjection) write(name string, data []byte, mode os.FileMode) error {
	path := filepath.Join(p.root, name)
	// Reconciliation retries must not replace the inode a candidate is validating.
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		if existing, err := os.OpenFile(path, os.O_RDWR, 0); err == nil { // #nosec G304 -- fixed basename in the daemon-owned directory, regular file checked above.
			old, err := io.ReadAll(io.LimitReader(existing, int64(len(data))+1))
			if err == nil && bytes.Equal(old, data) {
				err = p.secure(existing, mode)
				_ = existing.Close()
				return err
			}
			_ = existing.Close()
		}
	}
	f, err := os.CreateTemp(p.root, ".projection-*")
	if err != nil {
		return errors.New("create tunnel projection file failed")
	}
	defer func() { _ = os.Remove(f.Name()) }()
	defer func() { _ = f.Close() }()
	if err := p.secure(f, mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.New("write tunnel projection failed")
	}
	if err := f.Sync(); err != nil {
		return errors.New("sync tunnel projection failed")
	}
	if err := f.Close(); err != nil {
		return errors.New("close tunnel projection failed")
	}
	if err := os.Rename(f.Name(), filepath.Join(p.root, name)); err != nil {
		return errors.New("publish tunnel projection failed")
	}
	return p.syncDir()
}

func (p *FileProjection) secure(f *os.File, mode os.FileMode) error {
	if err := f.Chmod(mode); err != nil {
		return errors.New("set projection permissions failed")
	}
	if p.uid >= 0 || p.gid >= 0 {
		if err := f.Chown(p.uid, p.gid); err != nil {
			return errors.New("set projection ownership failed")
		}
	}
	if err := f.Sync(); err != nil {
		return errors.New("sync projection permissions failed")
	}
	return nil
}

func (p *FileProjection) syncDir() error {
	// Windows has no directory fsync; the appliance's Linux filesystem does.
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(p.root)
	if err != nil {
		return errors.New("open projection directory failed")
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return errors.New("sync projection directory failed")
	}
	return nil
}

var _ Projection = (*FileProjection)(nil)
