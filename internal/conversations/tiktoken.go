package conversations

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// cl100kBaseBPE is the cl100k_base BPE vocabulary, VENDORED into the repo so the
// encoder NEVER fetches it over the network at first use (RESEARCH Assumption A6;
// feedback_minipc_cpu_budget — no runtime network dependency, CI-deterministic).
// The file is the verbatim openaipublic cl100k_base.tiktoken blob (base64-token +
// space + rank per line). The default tiktoken-go loader would HTTP GET this same
// blob; the embedded loader below serves it from memory instead.
//
//go:embed cl100k/cl100k_base.tiktoken
var cl100kBaseBPE []byte

// embeddedBpeLoader is a tiktoken.BpeLoader that returns the vendored cl100k_base
// ranks from the embedded blob, ignoring the (network) blobpath the library would
// otherwise fetch. This is the offline-vocab mechanism (no new dependency on
// tiktoken-go-loader): we implement the one-method loader interface directly.
type embeddedBpeLoader struct{}

func (embeddedBpeLoader) LoadTiktokenBpe(string) (map[string]int, error) {
	return parseTiktokenBPE(cl100kBaseBPE)
}

// parseTiktokenBPE parses the base64-rank vocab format (mirrors tiktoken-go's own
// loadTiktokenBpe, but over the embedded bytes).
func parseTiktokenBPE(contents []byte) (map[string]int, error) {
	ranks := make(map[string]int, 100256)
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed cl100k_base line %q", line)
		}
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, fmt.Errorf("decode cl100k_base token: %w", err)
		}
		rank, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("parse cl100k_base rank: %w", err)
		}
		ranks[string(token)] = rank
	}
	return ranks, nil
}

// encoderOnce + cachedEncoder give a single, lazily-initialized cl100k_base
// encoder for the whole process (D-A2-06: init once, goleak-safe — no goroutine,
// no network). The first call configures the embedded loader BEFORE GetEncoding so
// the vocab is served from memory. Subsequent calls reuse the cached encoder.
var (
	encoderOnce sync.Once
	cachedEnc   *tiktoken.Tiktoken
	encoderErr  error
)

func encoder() (*tiktoken.Tiktoken, error) {
	encoderOnce.Do(func() {
		tiktoken.SetBpeLoader(embeddedBpeLoader{})
		cachedEnc, encoderErr = tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	})
	return cachedEnc, encoderErr
}

// countTokens estimates the cl100k_base token count of a string. The estimate is a
// fast ~5-10% approximation used ONLY for L2/L2.5 budget gating (SPEC Constraints
// — not billed accuracy). EncodeOrdinary skips special-token handling (none of the
// history is trusted to carry real special tokens).
func countTokens(enc *tiktoken.Tiktoken, text string) int {
	if text == "" {
		return 0
	}
	return len(enc.EncodeOrdinary(text))
}
