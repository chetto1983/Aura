package conversations

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"iter"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

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
	for line := range strings.SplitSeq(string(contents), "\n") {
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

// InitEncoder eagerly initializes the cached cl100k_base encoder at boot (the
// composition root calls it after db.Open, before serving) so the first turn does
// not pay the one-time vocab-parse latency. It is a thin exported wrapper over the
// lazy encoder() — idempotent (sync.Once), goleak-safe (no goroutine, no network).
func InitEncoder() error {
	_, err := encoder()
	return err
}

// countTokens estimates the cl100k_base token count of a string. The estimate is a
// fast ~5-10% approximation used ONLY for L2/L2.5 budget gating (SPEC Constraints
// — not billed accuracy). EncodeOrdinary skips special-token handling (none of the
// history is trusted to carry real special tokens).
//
// The ladder counts the whole history on every turn, and all of it but the newest turns was
// counted on the turn before, so a count is remembered (tokenCounts). The encoder sees the
// text in boundedPieces: tiktoken-go's byte-pair merge is quadratic in the length of one
// regex piece. Measured 2026-10-27, 64 KiB of base64 over zeroed bytes, one run of 'A' as
// an attachment encodes, took 3.2 s where prose takes 19 ms.
func countTokens(enc *tiktoken.Tiktoken, text string) int {
	if text == "" {
		return 0
	}
	if n, ok := tokenCounts.get(text); ok {
		return n
	}
	n := 0
	for piece := range boundedPieces(text, maxUnbrokenRun) {
		n += len(enc.EncodeOrdinary(piece))
	}
	tokenCounts.put(text, n)
	return n
}

// CountTokens is countTokens on the process encoder, for the callers outside this package
// that budget against the same estimate: the runner's fixed request overhead.
func CountTokens(text string) (int, error) {
	enc, err := encoder()
	if err != nil {
		return 0, err
	}
	return countTokens(enc, text), nil
}

// maxUnbrokenRun is the longest run of one character class the encoder is handed whole. No
// prose, JSON or base64 of varied bytes comes near it, so their counts are exact; a longer run
// is counted in pieces, which can only add a token per cut.
const maxUnbrokenRun = 256

// boundedPieces yields text in consecutive pieces, cutting only inside a run of one
// character class (letters, digits, whitespace, anything else) that has reached limit bytes.
// The cl100k pattern splits on class changes, so a cut there splits only a regex piece that
// was itself longer than the limit. A cut never falls inside a rune.
func boundedPieces(text string, limit int) iter.Seq[string] {
	return func(yield func(string) bool) {
		start, run, class := 0, 0, -1
		for i, r := range text {
			c := runeClass(r)
			if c != class {
				class, run = c, 0
			}
			if run >= limit {
				if !yield(text[start:i]) {
					return
				}
				start, run = i, 0
			}
			run += utf8.RuneLen(r)
		}
		yield(text[start:])
	}
}

const (
	classLetter = iota
	classDigit
	classSpace
	classOther
)

func runeClass(r rune) int {
	switch {
	case unicode.IsLetter(r):
		return classLetter
	case unicode.IsNumber(r):
		return classDigit
	case unicode.IsSpace(r):
		return classSpace
	default:
		return classOther
	}
}
