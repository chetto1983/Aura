package conversations

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

func (s *Store) readTurnSidecar(conversationID string, seq int) ([]byte, error) {
	return s.readTurnSidecarReserved(conversationID, seq, 0, nil)
}

// readTurnSidecarReserved reconstructs the write-once path from trusted row identity,
// opens it through os.Root's no-symlink boundary, then reserves its declared size before
// allocating. Managed history supplies both caps; raw export/history reads deliberately do
// not because their contract is to return the complete persisted turn.
func (s *Store) readTurnSidecarReserved(
	conversationID string,
	seq int,
	maxBytes int64,
	reserve func(int64) error,
) ([]byte, error) {
	if !filepath.IsAbs(s.runDir) {
		return nil, fmt.Errorf("sidecar runDir %q is not absolute", s.runDir)
	}
	if err := validateID("conversation_id", conversationID); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.runDir)
	if err != nil {
		return nil, fmt.Errorf("open run root %q: %w", s.runDir, err)
	}
	defer func() { _ = root.Close() }()
	rel := path.Join("conversations", conversationID, fmt.Sprintf("%d.content", seq))
	file, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", rel)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("sidecar is %d bytes, limit %d", info.Size(), maxBytes)
	}
	if reserve != nil {
		if err := reserve(info.Size()); err != nil {
			return nil, err
		}
	}
	if maxBytes <= 0 {
		return io.ReadAll(file)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("sidecar grew beyond limit %d while reading", maxBytes)
	}
	if int64(len(data)) != info.Size() {
		return nil, fmt.Errorf("sidecar size changed while reading: stat=%d read=%d",
			info.Size(), len(data))
	}
	return data, nil
}
