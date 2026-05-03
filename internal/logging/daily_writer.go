package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyWriter is an io.Writer that writes to a date-stamped file in dir,
// rotating at midnight and removing files older than maxAge.
// It acts as a circular buffer of one day deep.
type dailyWriter struct {
	mu      sync.Mutex
	dir     string
	maxAge  time.Duration
	current string // current date YYYY-MM-DD
	file    *os.File
}

// newDailyWriter creates a daily-rotating writer. dir is the log directory;
// maxAge is how long log files are kept (e.g. 24h for one day deep).
func newDailyWriter(dir string, maxAge time.Duration) (*dailyWriter, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("dailyWriter: create dir %s: %w", dir, err)
	}
	dw := &dailyWriter{dir: dir, maxAge: maxAge}
	if err := dw.rotate(time.Now()); err != nil {
		return nil, err
	}
	// Clean stale files on startup.
	dw.clean()
	return dw, nil
}

func (dw *dailyWriter) Write(p []byte) (int, error) {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")
	if today != dw.current {
		if err := dw.rotate(now); err != nil {
			return 0, err
		}
		dw.clean()
	}

	if dw.file == nil {
		return 0, os.ErrClosed
	}
	return dw.file.Write(p)
}

func (dw *dailyWriter) rotate(now time.Time) error {
	if dw.file != nil {
		dw.file.Close()
		dw.file = nil
	}

	// Commit the date only after the new file is open — if OpenFile fails
	// we stay on the old date and retry on the next Write.
	newDate := now.Format("2006-01-02")
	name := fmt.Sprintf("aura-%s.log", newDate)
	path := filepath.Join(dw.dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("dailyWriter: open %s: %w", path, err)
	}
	dw.current = newDate
	dw.file = f
	return nil
}

// clean removes aura-*.log files older than maxAge.
func (dw *dailyWriter) clean() {
	entries, err := os.ReadDir(dw.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-dw.maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isLogFile(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dw.dir, name))
		}
	}
}

func isLogFile(name string) bool {
	return len(name) > 9 && name[:5] == "aura-" && name[len(name)-4:] == ".log"
}

func (dw *dailyWriter) Close() error {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	if dw.file == nil {
		return nil
	}
	err := dw.file.Close()
	dw.file = nil
	return err
}
