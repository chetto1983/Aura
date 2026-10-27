package conversations

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func piecesOf(text string, limit int) []string {
	return slices.Collect(boundedPieces(text, limit))
}

// TestBoundedPieces_LeavesOrdinaryTextWhole: prose, compact JSON and base64 of random bytes
// never hold a run of one character class as long as the limit, so they reach the encoder in
// one piece and keep the count they always had.
func TestBoundedPieces_LeavesOrdinaryTextWhole(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"prose":   strings.Repeat("Il cockpit importa ogni ramo, e il picker li percorre. ", 40),
		"json":    strings.Repeat(`{"title":"Result","url":"https://example.test/a/b?q=1"},`, 40),
		"base64":  base64.StdEncoding.EncodeToString([]byte(strings.Repeat("\x01\x93\xfe\x22\x7f", 400))),
		"unicode": strings.Repeat("città più già ", 60),
	} {
		if got := piecesOf(text, 256); len(got) != 1 || got[0] != text {
			t.Errorf("%s was cut into %d pieces", name, len(got))
		}
	}
}

func TestBoundedPieces_CutsARunOfOneClassAtTheLimit(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]string{
		"letters":     strings.Repeat("A", 1000),
		"punctuation": strings.Repeat("=", 1000),
		"whitespace":  strings.Repeat(" ", 1000),
	} {
		text := "head " + run + " tail"
		got := piecesOf(text, 256)
		if strings.Join(got, "") != text {
			t.Fatalf("%s: the pieces do not rebuild the text", name)
		}
		for i, piece := range got {
			if longestRun(piece) > 256 {
				t.Errorf("%s: piece %d holds a run of %d bytes", name, i, longestRun(piece))
			}
		}
		if len(got) < 4 {
			t.Errorf("%s: %d pieces, want the 1000-byte run cut at least three times", name, len(got))
		}
	}
}

func TestBoundedPieces_NeverSplitsARune(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("à", 700)
	for _, piece := range piecesOf(text, 255) {
		if !utf8.ValidString(piece) {
			t.Fatalf("a piece cut a two-byte rune: %q", piece[:4])
		}
	}
}

func longestRun(text string) int {
	longest, run, last := 0, 0, -1
	for _, r := range text {
		class := runeClass(r)
		if class != last {
			run = 0
		}
		run += utf8.RuneLen(r)
		last = class
		longest = max(longest, run)
	}
	return longest
}

// TestCountTokens_ExactOnOrdinaryText: the bound changes no count the ladder already made.
func TestCountTokens_ExactOnOrdinaryText(t *testing.T) {
	t.Parallel()
	enc := mustEncoderRaw(t)
	text := strings.Repeat(`Il ramo scritto per ultimo {"seq":12,"parent":11} https://example.test/x?y=1 `, 50)
	if got, want := countTokens(enc, text), len(enc.EncodeOrdinary(text)); got != want {
		t.Fatalf("count = %d, want the encoder's own %d", got, want)
	}
}

// TestCountTokens_ALongRunCostsWhatItsPiecesCost: a run longer than the bound is counted as
// its pieces. The estimate may only grow, by at most one token per cut, so a budget built on
// it errs on the side of room.
func TestCountTokens_ALongRunCostsWhatItsPiecesCost(t *testing.T) {
	t.Parallel()
	enc := mustEncoderRaw(t)
	text := "zeros: " + strings.Repeat("A", 4*maxUnbrokenRun)
	exact := len(enc.EncodeOrdinary(text))
	got := countTokens(enc, text)
	cuts := len(piecesOf(text, maxUnbrokenRun)) - 1
	if got < exact || got > exact+cuts {
		t.Fatalf("count = %d, want between the exact %d and %d", got, exact, exact+cuts)
	}
}

func TestTokenCountCache_RemembersAndStaysBounded(t *testing.T) {
	t.Parallel()
	cache := newTokenCountCache(4)
	cache.put("alpha", 3)
	if n, ok := cache.get("alpha"); !ok || n != 3 {
		t.Fatalf("get(alpha) = %d, %v", n, ok)
	}
	if _, ok := cache.get("beta"); ok {
		t.Fatal("a text never counted was found")
	}
	for i, text := range []string{"b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		cache.put(text, i)
	}
	if size := len(cache.current) + len(cache.previous); size > 8 {
		t.Fatalf("the cache holds %d counts, want at most two generations of 4", size)
	}
	if _, ok := cache.get("alpha"); ok {
		t.Fatal("a count two generations old survived")
	}
	if n, ok := cache.get("j"); !ok || n != 8 {
		t.Fatalf("the newest count = %d, %v", n, ok)
	}
}

func TestTokenCountCache_PromotesAHitFromThePreviousGeneration(t *testing.T) {
	t.Parallel()
	cache := newTokenCountCache(2)
	cache.put("kept", 7)
	cache.put("x", 1)
	cache.put("y", 2) // rolls "kept" into the previous generation
	if n, ok := cache.get("kept"); !ok || n != 7 {
		t.Fatalf("get(kept) = %d, %v", n, ok)
	}
	cache.put("z", 3)
	cache.put("w", 4) // a second roll drops the old previous generation
	if n, ok := cache.get("kept"); !ok || n != 7 {
		t.Fatalf("a count read back was not kept: %d, %v", n, ok)
	}
}

func pieceLengths(text string, limit int) []int {
	var out []int
	for _, piece := range piecesOf(text, limit) {
		out = append(out, len(piece))
	}
	return out
}

// TestBoundedPieces_CutsExactlyAtTheLimitAndRestartsOnAClassChange pins the boundary: a run
// of exactly limit bytes is whole, the next byte of the same class starts a piece, and a new
// class starts counting from zero.
func TestBoundedPieces_CutsExactlyAtTheLimitAndRestartsOnAClassChange(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		text string
		want []int
	}{
		"at the limit":   {strings.Repeat("A", 8), []int{8}},
		"one past it":    {strings.Repeat("A", 9), []int{8, 1}},
		"two runs":       {strings.Repeat("A", 10) + strings.Repeat("=", 10), []int{8, 10, 2}},
		"a long run":     {strings.Repeat("A", 20), []int{8, 8, 4}},
		"a class change": {strings.Repeat("A", 8) + "=" + strings.Repeat("A", 8), []int{17}},
	} {
		if got := pieceLengths(tc.text, 8); !slices.Equal(got, tc.want) {
			t.Errorf("%s: piece lengths %v, want %v", name, got, tc.want)
		}
	}
}

func TestBoundedPieces_StopsWhenTheCallerDoes(t *testing.T) {
	t.Parallel()
	seen := 0
	for range boundedPieces(strings.Repeat("A", 100), 8) {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("yielded %d pieces after the caller stopped at one", seen)
	}
}

// TestCountTokens_ReadsTheCountItRemembered: a text counted once is answered from memory, so
// the turn after costs a hash per message rather than an encode.
func TestCountTokens_ReadsTheCountItRemembered(t *testing.T) {
	t.Parallel()
	enc := mustEncoderRaw(t)
	text := "a text only this test counts: " + t.Name()
	n := countTokens(enc, text)
	if cached, ok := tokenCounts.get(text); !ok || cached != n {
		t.Fatalf("remembered %d, %v; want %d", cached, ok, n)
	}
	tokenCounts.put(text, n+1000)
	if got := countTokens(enc, text); got != n+1000 {
		t.Fatalf("count = %d, want the remembered %d", got, n+1000)
	}
}

func TestTokenCounts_HoldsALongHistory(t *testing.T) {
	t.Parallel()
	enc := mustEncoderRaw(t)
	texts := make([]string, 2000)
	for i := range texts {
		texts[i] = fmt.Sprintf("%s turn %d", t.Name(), i)
		countTokens(enc, texts[i])
	}
	for _, text := range texts {
		if _, ok := tokenCounts.get(text); !ok {
			t.Fatalf("the count of %q was forgotten", text)
		}
	}
}

func TestParseTiktokenBPE_ReadsRanksAndRefusesAMalformedLine(t *testing.T) {
	t.Parallel()
	ranks, err := parseTiktokenBPE([]byte("QQ== 0\n\nQg== 1\n"))
	if err != nil || len(ranks) != 2 || ranks["A"] != 0 || ranks["B"] != 1 {
		t.Fatalf("ranks = %v, %v; want A=0 and B=1 across the blank line", ranks, err)
	}
	if _, err := parseTiktokenBPE([]byte("QQ==\n")); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("a line with no rank = %v, want it refused as malformed", err)
	}
}
