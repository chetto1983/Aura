package filecard

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

// htmlTextBreaks end a run of text in markup. They are used both for real HTML
// and for the XHTML inside an EPUB.
var htmlTextBreaks = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"div": true, "li": true, "title": true, "td": true, "th": true, "br": true,
}

// maxMarkupBytes bounds the markup parsed for one page.
const maxMarkupBytes = 8 << 20

// buildHTML describes a page by its title, its headings and its opening words.
// Headings are the page's own outline, which is the closest thing a web page
// has to the column headers of a spreadsheet.
func buildHTML(req Request, card Card) (Card, error) {
	file, err := os.Open(req.Path)
	if err != nil {
		return card, fmt.Errorf("open page: %w", err)
	}
	defer func() { _ = file.Close() }()

	root, err := html.Parse(io.LimitReader(file, maxMarkupBytes))
	if err != nil {
		return card, fmt.Errorf("parse page: %w", err)
	}
	title, headings, body := walkHTML(root)
	card.Title = clean(title)
	if len(headings) > 0 {
		card.Text = append(card.Text, "Headings: "+strings.Join(headings, "; "))
	}
	if len(body) > 0 {
		card.Text = append(card.Text, "Opens: "+strings.Join(firstWords(body, maxProseWords), " / "))
	}
	if card.Title == "" && len(headings) > 0 {
		card.Title = headings[0]
	}
	if len(headings) == 0 && len(body) == 0 {
		card.Caveats = append(card.Caveats, "The page carried no readable text at ingest.")
		return card, nil
	}
	card.Caveats = append(card.Caveats, "Only the page's outline and opening are described here.")
	return card, nil
}

// walkHTML collects the title, the headings and the body paragraphs, skipping
// the parts of a page that are machinery rather than content.
func walkHTML(root *html.Node) (title string, headings, body []string) {
	var visit func(node *html.Node)
	var current strings.Builder
	words := 0
	visit = func(node *html.Node) {
		switch node.Type {
		case html.ElementNode:
			switch node.Data {
			// Chrome, not content: a page's navigation, its footer and its forms
			// say what the SITE is, never what the page is about. Left in, a
			// Wikipedia article opens with "Vai al contenuto / Menu principale".
			case "script", "style", "noscript", "svg", "nav", "footer", "aside", "form":
				return
			case "title":
				if title == "" {
					title = nodeText(node)
				}
				return
			case "h1", "h2", "h3":
				if len(headings) < maxHeadings {
					if heading := clean(nodeText(node)); heading != "" {
						headings = append(headings, truncateRunes(heading, 90))
					}
				}
				return
			}
		case html.TextNode:
			if words < maxProseWords {
				current.WriteString(node.Data)
				current.WriteByte(' ')
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == html.ElementNode && htmlTextBreaks[node.Data] {
			if text := clean(current.String()); text != "" {
				body = append(body, text)
				words += len(strings.Fields(text))
			}
			current.Reset()
		}
	}
	visit(root)
	if text := clean(current.String()); text != "" {
		body = append(body, text)
	}
	return title, headings, body
}

func nodeText(node *html.Node) string {
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			b.WriteString(child.Data)
		}
		if child.Type == html.ElementNode {
			b.WriteString(nodeText(child))
		}
	}
	return clean(b.String())
}

// maxPlainTextBytes bounds a plain-text read. A log file can be gigabytes; its
// first pages say what it is.
const maxPlainTextBytes = 256 << 10

// buildPlainText describes text, markdown, JSON and XML: the markdown heading
// outline when there is one, then the opening words. Nothing here is parsed as
// data — a JSON file's first lines show its shape, which is what routing needs.
func buildPlainText(req Request, card Card) (Card, error) {
	file, err := os.Open(req.Path)
	if err != nil {
		return card, fmt.Errorf("open text: %w", err)
	}
	defer func() { _ = file.Close() }()

	source, err := io.ReadAll(io.LimitReader(file, maxPlainTextBytes))
	if err != nil {
		return card, fmt.Errorf("read text: %w", err)
	}
	headings := markdownHeadings(source)
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(source)))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	words := 0
	for scanner.Scan() {
		line := clean(scanner.Text())
		// A heading is already in the outline, and a line that merely looks like one is
		// not prose either. The outline itself no longer believes the look of a line.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if words < maxProseWords {
			lines = append(lines, line)
			words += len(strings.Fields(line))
		}
		if words >= maxProseWords {
			break
		}
	}
	if len(headings) > 0 {
		card.Title = headings[0]
		card.Text = append(card.Text, "Headings: "+strings.Join(headings, "; "))
	}
	if len(lines) > 0 {
		card.Text = append(card.Text, "Opens: "+strings.Join(firstWords(lines, maxProseWords), " / "))
	}
	if len(card.Text) == 0 {
		card.Caveats = append(card.Caveats, "The file carried no readable text at ingest.")
		return card, nil
	}
	card.Caveats = append(card.Caveats, "Only the opening of the file is described here.")
	return card, nil
}

// markdownHeadings is the document's outline, read by a CommonMark parser.
//
// It used to be "the line starts with #", which cannot tell a heading from a shell comment
// inside a fenced block: measured 2026-09-09, a test document carrying
// "# QUESTA RIGA NON DEVE DIVENTARE UN HEADING" between ```bash fences had exactly that
// line listed in its card as a section. The parser also reads what the pattern could not --
// setext headings, and the closing hashes of "## Titolo ##" that are not part of the title.
//
// goldmark is CommonMark-compliant and was already in the module graph. The heading's own
// Lines() carry the title without its marker, which is what the card shows.
func markdownHeadings(source []byte) []string {
	var headings []string
	document := goldmark.New().Parser().Parse(text.NewReader(source))
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		heading, ok := node.(*ast.Heading)
		if !entering || !ok || heading.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		segment := heading.Lines().At(0)
		if title := clean(string(segment.Value(source))); title != "" {
			headings = append(headings, truncateRunes(title, 90))
		}
		if len(headings) >= maxHeadings {
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil
	}
	return headings
}

// Media cards stay compact because their derived text lives in separately embedded
// retrieval passages. The card labels the original without pretending to contain it.
func buildImage(_ Request, card Card) (Card, error) {
	card.Caveats = append(card.Caveats,
		"The visual content is indexed in separate retrieval passages; this compact card only labels the original. "+
			"Open the image to verify its contents.")
	return card, nil
}

func buildAudio(_ Request, card Card) (Card, error) {
	card.Caveats = append(card.Caveats,
		"The transcript is indexed in separate retrieval passages; this compact card only labels the original. "+
			"Open the audio to verify its contents.")
	return card, nil
}
