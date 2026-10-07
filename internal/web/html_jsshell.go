package web

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// jsShellMaxRunes is the most visible body text a JavaScript shell carries. The
// SiePortal product page of 2026-10-07 had 10 ("Loading...") around its Angular
// <app-root>; a page that says a sentence of its own is above it and stays a
// low_content page whose text the model still gets.
const jsShellMaxRunes = 50

// errJavaScriptRequired tells the model the fetch reached the page and the page
// has nothing to give without a browser, so retrying web_fetch cannot help.
func errJavaScriptRequired() *WebError {
	return &WebError{
		Code:    CodeJavaScriptRequired,
		Message: "the page renders its content with JavaScript and the fetch returned only its empty shell; open it in a browser to read it",
	}
}

// isJavaScriptShell reports whether body is markup that renders nothing without
// JavaScript: it loads at least one script, and its <body>, read without script,
// style, noscript and template, holds under jsShellMaxRunes runes of text. Both
// halves are structure, never the wording of a "please enable JavaScript" plea.
func isJavaScriptShell(body []byte) bool {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return false
	}
	scripts, textRunes := 0, 0
	var walk func(n *html.Node, inBody bool)
	walk = func(n *html.Node, inBody bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script":
				scripts++
				return
			case "style", "noscript", "template":
				return
			case "body":
				inBody = true
			}
		}
		if n.Type == html.TextNode && inBody {
			textRunes += utf8.RuneCountInString(strings.TrimSpace(n.Data))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inBody)
		}
	}
	walk(doc, false)
	return scripts > 0 && textRunes < jsShellMaxRunes
}
