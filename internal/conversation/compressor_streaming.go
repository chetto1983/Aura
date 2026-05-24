package conversation

import "strings"

// StreamingContextScrubber is a stateful FSM that strips <memory-context>
// ... </memory-context> blocks from streaming text chunks.
// The scrubber preserves state across Feed() calls so blocks that span
// multiple SSE chunks are handled correctly.
//
// Usage:
//
//	s := &StreamingContextScrubber{}
//	for _, chunk := range chunks {
//	    if visible := s.Feed(chunk); visible != "" { emit(visible) }
//	}
//	if tail := s.Flush(); tail != "" { emit(tail) }
type StreamingContextScrubber struct {
	inSpan bool
	buf    string
}

const (
	_scrubOpenTag  = "<memory-context>"
	_scrubCloseTag = "</memory-context>"
)

// Reset clears all scrubber state for a new streaming response.
func (s *StreamingContextScrubber) Reset() {
	s.inSpan = false
	s.buf = ""
}

// Feed processes a streaming text chunk and returns the visible portion.
// Any trailing fragment that could be the start of a tag is held back in the
// internal buffer and surfaced on the next Feed() call or emitted by Flush().
func (s *StreamingContextScrubber) Feed(text string) string {
	if text == "" {
		return ""
	}
	buf := s.buf + text
	s.buf = ""
	var out strings.Builder

	for len(buf) > 0 {
		if s.inSpan {
			idx := scrubIndexFold(buf, _scrubCloseTag)
			if idx == -1 {
				// Hold back a potential partial close-tag suffix; discard the rest.
				if held := scrubMaxPartialSuffix(buf, _scrubCloseTag); held > 0 {
					s.buf = buf[len(buf)-held:]
				}
				return out.String()
			}
			buf = buf[idx+len(_scrubCloseTag):]
			s.inSpan = false
		} else {
			idx := scrubIndexFold(buf, _scrubOpenTag)
			if idx == -1 {
				// No open tag — hold back a potential partial open-tag suffix.
				if held := scrubMaxPartialSuffix(buf, _scrubOpenTag); held > 0 {
					out.WriteString(buf[:len(buf)-held])
					s.buf = buf[len(buf)-held:]
				} else {
					out.WriteString(buf)
				}
				return out.String()
			}
			if idx > 0 {
				out.WriteString(buf[:idx])
			}
			buf = buf[idx+len(_scrubOpenTag):]
			s.inSpan = true
		}
	}
	return out.String()
}

// Flush emits any held-back buffer at end-of-stream.
// If we are still inside an unterminated span the remaining content is
// discarded — leaking partial memory context is worse than a truncated answer.
func (s *StreamingContextScrubber) Flush() string {
	if s.inSpan {
		s.buf = ""
		s.inSpan = false
		return ""
	}
	tail := s.buf
	s.buf = ""
	return tail
}

// scrubIndexFold returns the byte index of tag inside s, case-insensitively.
// Both s and tag are expected to be ASCII-range for the tag portion; using
// strings.ToLower is safe because the tags contain only ASCII characters so
// byte lengths are preserved in the lowercased copy.
func scrubIndexFold(s, tag string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(tag))
}

// scrubMaxPartialSuffix returns the length of the longest suffix of buf that
// is a prefix of tag (case-insensitive). Returns 0 when no suffix matches.
func scrubMaxPartialSuffix(buf, tag string) int {
	tagLow := strings.ToLower(tag)
	bufLow := strings.ToLower(buf)
	check := min(len(tagLow)-1, len(bufLow))
	for i := check; i > 0; i-- {
		if strings.HasPrefix(tagLow, bufLow[len(bufLow)-i:]) {
			return i
		}
	}
	return 0
}
