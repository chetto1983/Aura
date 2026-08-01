package arcadedb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// SpacyExtractor talks to docker/arcadedb-mcp/spacy_worker.py over a pipe: one
// JSON request per line, one reply per line, model loaded once at boot.
//
// A pipe rather than cgo (go-spacy embeds an interpreter and pins build and
// runtime to one libpython ABI) and rather than an HTTP sidecar (one more
// container to run and version). It also keeps `go build ./...` free of cgo,
// which the build-tagged alternative could not.
type SpacyExtractor struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	language string
	// fallback answers when the worker is absent or has failed. Extraction
	// degrading to capitalised runs is worse retrieval; it is not an outage.
	fallback RegexExtractor
	broken   bool
}

// NewSpacyExtractor starts the worker from a full argv, so the same code serves
// the server running INSIDE the image (`python3 /opt/arcadedb-mcp/spacy_worker.py`)
// and a caller outside it reaching the image's interpreter
// (`docker exec -i aura-arcadedb-mcp python /opt/arcadedb-mcp/spacy_worker.py`).
// The models live in one place either way, which is the point of putting them
// in the image at all.
func NewSpacyExtractor(argv []string, language string) (*SpacyExtractor, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("arcadedb: spacy worker command must be non-empty")
	}
	if strings.TrimSpace(language) == "" {
		language = "en"
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // operator-configured worker command.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("arcadedb: spacy worker stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("arcadedb: spacy worker stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("arcadedb: start spacy worker: %w", err)
	}
	return &SpacyExtractor{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReaderSize(stdout, 1<<20),
		language: language,
	}, nil
}

// Close stops the worker.
func (s *SpacyExtractor) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil {
		return s.cmd.Wait()
	}
	return nil
}

// Entities extracts from one text.
func (s *SpacyExtractor) Entities(text string) []string {
	batch := s.EntitiesBatch([]string{text})
	if len(batch) == 0 {
		return nil
	}
	return batch[0]
}

// EntitiesBatch is the call that matters: spaCy's own pipe() amortises the
// per-document cost, and ingest always has a batch.
func (s *SpacyExtractor) EntitiesBatch(texts []string) [][]string {
	if len(texts) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return s.fallbackAll(texts)
	}
	request, err := json.Marshal(map[string]any{"texts": texts, "lang": s.language})
	if err != nil {
		return s.fallbackAll(texts)
	}
	if _, err = s.stdin.Write(append(request, '\n')); err != nil {
		s.broken = true
		return s.fallbackAll(texts)
	}
	line, err := s.stdout.ReadBytes('\n')
	if err != nil {
		s.broken = true
		return s.fallbackAll(texts)
	}
	var reply struct {
		Entities [][]string `json:"entities"`
		Error    string     `json:"error"`
	}
	if json.Unmarshal(line, &reply) != nil || reply.Error != "" ||
		len(reply.Entities) != len(texts) {
		return s.fallbackAll(texts)
	}
	return reply.Entities
}

func (s *SpacyExtractor) fallbackAll(texts []string) [][]string {
	out := make([][]string, 0, len(texts))
	for _, text := range texts {
		out = append(out, s.fallback.Entities(text))
	}
	return out
}
