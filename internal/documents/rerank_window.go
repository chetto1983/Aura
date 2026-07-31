package documents

import (
	"regexp"
	"strings"
	"unicode"
)

// focusWindowRunes is how much of a chunk survives around the part that matched. It is
// sized against the two windows downstream that are NOT negotiable: the reranker reads
// the first 480 runes of whatever it is handed (maxRerankDocChars, itself forced by the
// sidecar answering 500 past ~512 tokens), and the agent's tool-result preview is 2048
// bytes. A window under both means the same text is what the reranker judged AND what
// the model reads — no third truncation with a different idea of where the answer is.
const focusWindowRunes = 1400

// minFocusTermRunes ignores query tokens too short to carry meaning. "di", "il", "la"
// match half the corpus and would make every segment look equally good, which is the
// failure this whole function exists to fix.
const minFocusTermRunes = 3

var rowSegmentPattern = regexp.MustCompile(`(?m)^|(?:row \d+: )`)

// focusWindow narrows a chunk to the part that actually answers the query, keeping the
// matching segment FIRST so it survives every downstream truncation.
//
// Measured on the live deployment 2026-07-30, this is the difference between retrieval
// working and not. A spreadsheet chunk is ~6400 characters — about 50 rows — and the
// reranker sees its first 480, which is rows 5851-5853. Asked for a client on row 5882
// the reranker scored that chunk 0.0586, while scoring the row itself 0.9933: it
// separates signal from noise by 14000x when it can see the answer and by 1x when it
// cannot. Worse, the chunk that WON was the one holding the header row, because
// "Cliente | Ragione sociale | Località" contains the words a question is phrased in —
// the ranking was by resemblance to the SCHEMA, not by presence of the ANSWER.
//
// The selection is lexical on purpose. It costs no I/O and no model call, and exact
// term overlap is precisely the signal an embedding averages away on rare tokens: the
// same reason a vector search misses a company name it has never seen. The reranker
// still decides relevance — this only decides which 1400 characters it gets to judge.
//
// A chunk with no term overlap keeps its head, so a query the corpus cannot answer
// behaves exactly as before and is left for the relevance floor to drop.
func focusWindow(text, query string) string {
	if len([]rune(text)) <= focusWindowRunes {
		return text
	}
	terms := focusTerms(query)
	if len(terms) == 0 {
		return truncateFocus(text)
	}
	segments := splitSegments(text)
	best, bestScore := -1, 0
	for i, seg := range segments {
		if score := segmentScore(seg, terms); score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return truncateFocus(text)
	}
	return truncateFocus(strings.Join(segments[best:], ""))
}

// splitSegments cuts a chunk into the units a match can land in. Spreadsheet chunks
// carry an explicit `row N: ` marker, which is the only row delimiter that survives the
// extractor's whitespace normalisation; prose falls back to lines. Delimiters are kept
// with their segment so rejoining is lossless and the row number stays citable.
func splitSegments(text string) []string {
	if idx := rowSegmentPattern.FindAllStringIndex(text, -1); len(idx) > 1 {
		out := make([]string, 0, len(idx))
		for i, loc := range idx {
			end := len(text)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			if seg := text[loc[0]:end]; seg != "" {
				out = append(out, seg)
			}
		}
		if len(out) > 1 {
			return out
		}
	}
	return []string{text}
}

func focusTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) < minFocusTermRunes {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

// segmentScore counts DISTINCT query terms present, not occurrences: a row repeating one
// term must never outrank a row carrying several. That is the same guard the doc-scoped
// sparse query already applies (`size([term IN $terms WHERE ...])`), kept consistent so
// the two stages cannot disagree about what "more relevant" means.
func segmentScore(segment string, terms []string) int {
	lower := strings.ToLower(segment)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score++
		}
	}
	return score
}

func truncateFocus(text string) string {
	runes := []rune(text)
	if len(runes) <= focusWindowRunes {
		return text
	}
	return string(runes[:focusWindowRunes])
}

// focusSeeds narrows every seed to its query-relevant window before the rerank call.
//
// It returns a new slice and never mutates the input: the focused text is what the
// reranker judges AND what the caller returns, so the model reads exactly the passage
// the score was assigned to. Letting those diverge is how a chunk could rank first on
// evidence the model then never saw — the seed order and the visible text disagreeing
// about which 480 characters mattered.
func focusSeeds(seeds []SearchHit, query string) []SearchHit {
	focused := make([]SearchHit, len(seeds))
	copy(focused, seeds)
	for i := range focused {
		focused[i].Text = focusWindow(focused[i].Text, query)
	}
	return focused
}
