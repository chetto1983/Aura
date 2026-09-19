package multimodal

import (
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// speechMarkdown parses answers the way the cockpit renders them: CommonMark plus the
// GFM extensions (tables, strikethrough, task lists, and linkify, which turns a bare
// URL into an AutoLink node the walk can drop). goldmark is already in the module
// graph for internal/documents/filecard.
//
// This replaced two hand-written regex strippers, one per channel, which had drifted:
// the web lane read emoji aloud, the Telegram lane read bare URLs aloud, dictated code
// blocks and turned user_id into userid. A parser cannot drift that way — a fence is a
// fence and an intraword underscore is not emphasis, by the spec rather than by a
// pattern someone has to remember to update in two places.
var speechMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// SpeechText reduces an answer to the prose a speech synthesizer should read: every
// channel that speaks goes through it. Markup disappears and its readable part stays
// (a link's label, an image's alt, inline code's text); what cannot be read aloud is
// dropped whole (code blocks, raw HTML, URLs, task boxes, emoji). Each block lands on
// its own line, which a synthesizer reads as a pause. "" means nothing speakable was
// left — an emoji-only or code-only answer — so the caller can skip synthesis instead
// of paying for silence.
func SpeechText(markdown string) string {
	source := []byte(markdown)
	document := speechMarkdown.Parser().Parse(text.NewReader(source))
	var spoken strings.Builder
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n := node.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock, *ast.AutoLink, *extast.TaskCheckBox:
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			// "riga<br>successiva" must not fuse into one word.
			if entering {
				spoken.WriteByte(' ')
			}
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			if entering {
				spoken.Write(n.Value(source))
				switch {
				case n.HardLineBreak():
					spoken.WriteByte('\n')
				case n.SoftLineBreak():
					spoken.WriteByte(' ')
				}
			}
		case *ast.String:
			if entering {
				spoken.Write(n.Value)
			}
		case *extast.TableCell:
			// Cells read as a list, rows as lines: "a, b" then "uno, due".
			if !entering && n.NextSibling() != nil {
				spoken.WriteString(", ")
			}
		default:
			if !entering && node.Type() == ast.TypeBlock {
				spoken.WriteByte('\n')
			}
		}
		return ast.WalkContinue, nil
	})
	return tidySpeech(stripNonSpeechRunes(spoken.String()))
}

// PrepareSpeech is what a TTS caller wants: the speakable text, capped at maxChars
// RUNES, and whether the cap bit. Normalizing before capping puts the cut on real prose
// instead of inside markup. A non-positive maxChars disables the cap: the Telegram lane
// passes 0 today, and giving it a ceiling is that argument rather than a new code path.
func PrepareSpeech(markdown string, maxChars int) (spoken string, truncated bool) {
	spoken = SpeechText(markdown)
	if maxChars <= 0 {
		return spoken, false
	}
	runes := []rune(spoken)
	if len(runes) <= maxChars {
		return spoken, false
	}
	return string(runes[:maxChars]), true
}

// stripNonSpeechRunes drops emoji and symbol runes (Unicode So and Sk, which hold
// 😊/✅/🟡 and the skin-tone modifiers) plus the ZWJ and variation selectors that glue
// emoji sequences together. Math symbols (Sm: + < = ~) are kept: a listener expects
// "2 + 2 = 4" read as words.
func stripNonSpeechRunes(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0x200D, r >= 0xFE00 && r <= 0xFE0F:
			return -1
		case unicode.Is(unicode.So, r), unicode.Is(unicode.Sk, r):
			return -1
		}
		return r
	}, s)
}

// tidySpeech collapses the whitespace the walk and the rune strip leave behind: runs of
// spaces become one, every line is trimmed, and empty lines go.
func tidySpeech(s string) string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		if words := strings.Fields(line); len(words) > 0 {
			lines = append(lines, strings.Join(words, " "))
		}
	}
	return strings.Join(lines, "\n")
}
