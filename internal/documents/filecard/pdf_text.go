package filecard

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/pdftext"
)

// A PDF's words come from poppler through internal/pdftext. That is the one step the
// card could never take for itself, and the reason the Normattiva decrees used to card
// as nothing but a file name, a size and a page count. Where the binary is absent — a
// developer host, a runner without poppler-utils — the card degrades to metadata and
// says so, which is what every PDF card did before this.

const (
	// pdfTextPages bounds the pages read. Measured on the reference corpus'
	// 830-page, 30 MB manual: 8 pages cost 181 ms and 20 cost 223 ms, because
	// poppler's fixed cost of opening the document dominates its per-page cost.
	// Page 20 is where that manual's table of contents ends and chapter 1
	// begins, so the cap is set by where the front matter stops summarising the
	// whole file, not by the clock.
	pdfTextPages = 20
	// pdfTextTimeout bounds a pathological or hostile file. The measured worst
	// case in the corpus is 0.22 s; a file that wants a hundred times that will
	// not produce a useful card either, and ingest must not wait for it.
	pdfTextTimeout = 20 * time.Second
	// pdfTermFloor is the frequency floor for a recurring term. A word said
	// three times across twenty pages is something the document keeps coming
	// back to; a word said once is a passer-by.
	pdfTermFloor = 3
	// pdfTermMinRunes drops the fragments a language-agnostic split leaves
	// behind — "n", "art", "dl". Longer function words need no list: a term
	// already spent in the opening is not repeated (see recurringTerms).
	pdfTermMinRunes = 4
	// pdfTerms bounds the recurring terms printed. A single spreadsheet column
	// gets six values; a whole document's vocabulary gets twice that, and it is
	// still one line of a card.
	pdfTerms = 12
	// pdfTitleMinWords is the shortest block that can serve as a title. A
	// one-word block is a running head or a fragment of cover art — the 30 MB
	// manual's first page yields "&EJUJPO", a subset font with no usable
	// ToUnicode — and naming a file after one is worse than leaving it unnamed.
	pdfTitleMinWords = 2
)

// pdfExtractText returns the text of the first pdfTextPages pages and reports
// whether the byte cap cut it short. Build takes no context — it is a plain call on
// the ingest path — so the deadline is set here rather than inherited.
func pdfExtractText(path string) (text string, bytesCapped bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), pdfTextTimeout)
	defer cancel()
	return pdftext.Extract(ctx, path, pdfTextPages, maxTextChars)
}

// pdfUnreadReason turns an extraction failure into the sentence the card prints.
// Poppler's exit codes are documented (pdftotext(1) EXIT CODES): 1 is a file it
// could not open as a PDF, 3 is one whose permissions forbid extraction.
func pdfUnreadReason(err error) string {
	if err == nil {
		return "It holds no text a machine can read, so it is probably a scan."
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return pdfExitReason(exit.ExitCode())
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "No PDF text extractor is installed on this host."
	}
	return "The extractor could not be run."
}

func pdfExitReason(code int) string {
	switch code {
	case 1:
		return "It could not be opened as a PDF."
	case 3:
		return "Its permissions forbid extracting text."
	default:
		return "The extractor could not read it."
	}
}

// pdfBlocks groups extractor output into paragraph-shaped blocks. pdftotext ends
// every visual line with a newline and separates blocks with a blank line, so a
// blank line is the only paragraph boundary the output carries.
func pdfBlocks(text string) []string {
	var blocks []string
	for block := range strings.SplitSeq(text, "\n\n") {
		if cleaned := clean(block); cleaned != "" {
			blocks = append(blocks, cleaned)
		}
	}
	return blocks
}

// pdfTitle picks the block that reads as the document's own title: the first one
// wide enough to be a statement rather than a running head.
func pdfTitle(blocks []string) string {
	for _, block := range blocks {
		if len(strings.Fields(block)) >= pdfTitleMinWords {
			return truncateRunes(block, 160)
		}
	}
	return ""
}

// recurringTerms sketches everything past the opening, by the same frequency
// rule a spreadsheet column uses for its values. The two lists are kept
// DISJOINT, which is also Postgres' model — most_common_vals plus a histogram of
// what they do not cover — and it is what makes a stop-word list unnecessary:
// "di", "the", "della" are all spent in the first hundred and forty words, so
// what survives here is what the document says that its opening does not.
func recurringTerms(blocks, opening []string) []Value {
	spent := map[string]bool{}
	for _, block := range opening {
		for _, token := range termTokens(block) {
			spent[token] = true
		}
	}
	counts := map[string]int{}
	for _, block := range blocks {
		for _, token := range termTokens(block) {
			if spent[token] || utf8.RuneCountInString(token) < pdfTermMinRunes {
				continue
			}
			counts[token]++
		}
	}
	return topValues(counts, pdfTermFloor, pdfTerms)
}

// termTokens splits text the way the index will: lowercase runs of letters and
// digits, so "dell'amministrazione" counts as "dell" and "amministrazione" here
// and in the tsvector alike.
func termTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// logExtractFailure records what the operator would otherwise only find by
// reading a card: a host that lost its PDF extractor stops describing PDFs, and
// the cards say so one at a time while nothing says so once.
func logExtractFailure(fileName string, err error) {
	slog.Warn("filecard: a PDF's text could not be extracted at ingest",
		"file_name", fileName, "tool", pdftext.Tool, "err", err)
}
