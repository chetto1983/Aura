package retention

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LocalArtifacts is the hardened Aura-owned run-directory adapter. It never
// enumerates or resolves Tempo's block root.
type LocalArtifacts struct {
	Root       string
	IdentityID string
}

// Candidates returns expired top-level Aura-owned entries and skips symlinks.
func (l LocalArtifacts) Candidates(ctx context.Context, policy Policy, now time.Time) ([]Candidate, error) {
	if l.Root == "" || l.IdentityID == "" {
		return nil, fmt.Errorf("local retention adapter is not configured")
	}
	var candidates []Candidate
	for _, class := range []Class{ClassTemporary, ClassCrashArtifact, ClassFullTrace, ClassMetadataTrace} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ttl := policy.TTL[class]
		if ttl <= 0 {
			continue
		}
		root, err := l.classRoot(class)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read local retention class: %w", err)
		}
		cutoff := now.Add(-ttl)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := validateTrustedID("artifact", entry.Name()); err != nil {
				continue
			}
			info, err := os.Lstat(filepath.Join(root, entry.Name()))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("lstat local retention candidate: %w", err)
			}
			if info.Mode()&os.ModeSymlink != 0 || info.ModTime().After(cutoff) {
				continue
			}
			candidates = append(candidates, Candidate{
				IdentityID: l.IdentityID, ArtifactID: entry.Name(), Version: localVersion(info),
				Action: ActionDeleteArtifact, Class: class, Bytes: nonNegativeSize(info.Size()),
			})
		}
	}
	return candidates, nil
}

// Revalidate reconstructs the fixed class root and rejects replacement symlinks.
func (l LocalArtifacts) Revalidate(ctx context.Context, candidate Candidate) (Revalidation, error) {
	if err := ctx.Err(); err != nil {
		return Revalidation{}, err
	}
	if err := candidate.Validate(); err != nil {
		return Revalidation{}, err
	}
	if candidate.IdentityID != l.IdentityID {
		return Revalidation{Exists: true, Owned: false}, nil
	}
	path, err := l.candidatePath(candidate)
	if err != nil {
		return Revalidation{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Revalidation{Exists: false, Owned: true, Version: candidate.Version}, nil
		}
		return Revalidation{}, fmt.Errorf("lstat local retention candidate: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Revalidation{}, errors.New("local retention candidate is a symlink")
	}
	return Revalidation{Exists: true, Owned: true, Version: localVersion(info)}, nil
}

// Remove deletes one revalidated Aura-owned entry without following symlinks.
func (l LocalArtifacts) Remove(ctx context.Context, candidate Candidate) (RemovalResult, error) {
	if err := ctx.Err(); err != nil {
		return RemovalResult{}, err
	}
	path, err := l.candidatePath(candidate)
	if err != nil {
		return RemovalResult{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RemovalResult{Absent: true}, nil
		}
		return RemovalResult{}, fmt.Errorf("lstat local retention candidate: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return RemovalResult{}, errors.New("local retention candidate is a symlink")
	}
	if err := os.RemoveAll(path); err != nil {
		return RemovalResult{}, fmt.Errorf("remove local retention candidate: %w", err)
	}
	return RemovalResult{Bytes: candidate.Bytes}, nil
}

// Finalize is a no-op: the durable retention item itself is local-artifact metadata.
func (l LocalArtifacts) Finalize(context.Context, Candidate) error { return nil }

func (l LocalArtifacts) candidatePath(candidate Candidate) (string, error) {
	if err := candidate.Validate(); err != nil {
		return "", err
	}
	root, err := l.classRoot(candidate.Class)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, candidate.ArtifactID), nil
}

func (l LocalArtifacts) classRoot(class Class) (string, error) {
	switch class {
	case ClassTemporary:
		return filepath.Join(l.Root, "tmp"), nil
	case ClassCrashArtifact:
		return filepath.Join(l.Root, "crash"), nil
	case ClassFullTrace:
		return filepath.Join(l.Root, "traces", "full"), nil
	case ClassMetadataTrace:
		return filepath.Join(l.Root, "traces", "metadata"), nil
	default:
		return "", fmt.Errorf("unsupported local retention class %q", class)
	}
}

func localVersion(info os.FileInfo) int64 {
	return info.ModTime().UTC().UnixNano() ^ nonNegativeSize(info.Size())
}

func nonNegativeSize(size int64) int64 {
	if size < 0 {
		return 0
	}
	return size
}
