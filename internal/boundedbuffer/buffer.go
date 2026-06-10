package boundedbuffer

import "sync"

// DefaultLimit is the fallback byte cap for buffers created without a positive limit.
const DefaultLimit = 4096

// Buffer is a mutex-guarded io.Writer that retains only the newest limit bytes.
type Buffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

// New returns a Buffer that keeps at most limit newest bytes.
func New(limit int) *Buffer {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Buffer{limit: limit}
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if b.limit <= 0 {
		return written, nil
	}
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)
		return written, nil
	}
	b.buf = append(b.buf, p...)
	if extra := len(b.buf) - b.limit; extra > 0 {
		copy(b.buf, b.buf[extra:])
		b.buf = b.buf[:b.limit]
	}
	return written, nil
}

func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// Len returns the number of retained bytes.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}
