package sandbox

import "bytes"

const (
	maxArtifacts     = 10
	maxArtifactBytes = 5 << 20
)

func clipOutput(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...[truncated]"
}

type limitedBuffer struct {
	data      bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 {
		remaining := b.limit - int64(b.data.Len())
		if remaining <= 0 {
			b.truncated = true
			return len(p), nil
		}
		if int64(len(p)) > remaining {
			_, _ = b.data.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
	}
	_, _ = b.data.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b.truncated {
		return b.data.String() + "\n...[truncated]"
	}
	return b.data.String()
}
