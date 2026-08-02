package filecard

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// maxSlidesRead bounds how many slides a presentation card looks at. Slide
// titles are the routing signal; a deck's later slides repeat its earlier ones.
const maxSlidesRead = 40

// paragraphBreaks are the element names that end a run of text in WordprocessingML
// (w:p) and DrawingML (a:p). Matching on the local name keeps this independent
// of which prefix a producer chose.
var paragraphBreaks = map[string]bool{"p": true}

// buildDOCX describes a Word document from its core properties and the opening
// of its body. The body part is plain XML, so the words are readable without a
// converter — unlike a PDF, where the same text is glyph indices behind a font
// encoding.
func buildDOCX(req Request, card Card) (Card, error) {
	reader, err := zip.OpenReader(req.Path)
	if err != nil {
		return card, fmt.Errorf("open document: %w", err)
	}
	defer func() { _ = reader.Close() }()

	props := readCoreProperties(&reader.Reader)
	card.Title = props.Title
	card.Text = append(card.Text, props.describe()...)

	segments := zipXMLSegments(&reader.Reader, "word/document.xml", paragraphBreaks)
	if len(segments) == 0 {
		card.Caveats = append(card.Caveats, "The document body could not be read at ingest.")
		return card, nil
	}
	if card.Title == "" {
		card.Title = truncateRunes(segments[0], 160)
	}
	card.Facts = append(card.Facts, plural(len(segments), "paragraph", "paragraphs"))
	card.Text = append(card.Text, "Opens: "+strings.Join(firstWords(segments, maxProseWords), " / "))
	card.Caveats = append(card.Caveats, "Only the opening of the document is described here.")
	return card, nil
}

// buildPPTX describes a deck by its slide titles. The first text run on a slide
// is its title in every template, and a list of titles is what tells one deck
// from another.
func buildPPTX(req Request, card Card) (Card, error) {
	reader, err := zip.OpenReader(req.Path)
	if err != nil {
		return card, fmt.Errorf("open presentation: %w", err)
	}
	defer func() { _ = reader.Close() }()

	props := readCoreProperties(&reader.Reader)
	card.Title = props.Title
	card.Text = append(card.Text, props.describe()...)

	slides := slideParts(&reader.Reader)
	card.Facts = append(card.Facts, plural(len(slides), "slide", "slides"))
	read := slides
	if len(read) > maxSlidesRead {
		read = read[:maxSlidesRead]
		card.Caveats = append(card.Caveats, fmt.Sprintf(
			"Only the first %d slides were read.", maxSlidesRead))
	}
	var titles []string
	words := 0
	for _, name := range read {
		segments := zipXMLSegments(&reader.Reader, name, paragraphBreaks)
		if len(segments) == 0 {
			continue
		}
		title := truncateRunes(segments[0], 90)
		titles = append(titles, title)
		words += len(strings.Fields(title))
		if words >= maxProseWords*2 {
			break
		}
	}
	if len(titles) == 0 {
		card.Caveats = append(card.Caveats, "No slide text could be read at ingest.")
		return card, nil
	}
	if card.Title == "" {
		card.Title = titles[0]
	}
	card.Text = append(card.Text, "Slides: "+strings.Join(titles, "; "))
	return card, nil
}

// slideParts lists slide XML parts in slide order. Zip order is not slide order,
// and neither is lexical order once a deck passes ten slides.
func slideParts(reader *zip.Reader) []string {
	var names []string
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(file.Name, ".xml") {
			names = append(names, file.Name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return slideNumber(names[i]) < slideNumber(names[j]) })
	return names
}

func slideNumber(name string) int {
	digits := strings.TrimSuffix(strings.TrimPrefix(path.Base(name), "slide"), ".xml")
	number, err := strconv.Atoi(digits)
	if err != nil {
		return 1 << 30
	}
	return number
}

// buildEPUB describes a book from its package metadata and the opening of its
// first spine document. An EPUB states its own title, author and subjects — the
// card does not have to infer them.
func buildEPUB(req Request, card Card) (Card, error) {
	reader, err := zip.OpenReader(req.Path)
	if err != nil {
		return card, fmt.Errorf("open book: %w", err)
	}
	defer func() { _ = reader.Close() }()

	opfPath := epubPackagePath(&reader.Reader)
	if opfPath == "" {
		card.Caveats = append(card.Caveats, "The book declares no package document; it was not read at ingest.")
		return card, nil
	}
	meta, first := readEPUBPackage(&reader.Reader, opfPath)
	card.Title = meta.Title
	card.Text = append(card.Text, meta.describe()...)
	if first == "" {
		card.Caveats = append(card.Caveats, "Only the book's metadata was read at ingest.")
		return card, nil
	}
	segments := zipXMLSegments(&reader.Reader, first, htmlTextBreaks)
	if len(segments) > 0 {
		if card.Title == "" {
			card.Title = truncateRunes(segments[0], 160)
		}
		card.Text = append(card.Text, "Opens: "+strings.Join(firstWords(segments, maxProseWords), " / "))
	}
	card.Caveats = append(card.Caveats, "Only the book's metadata and opening pages are described here.")
	return card, nil
}

// epubPackagePath follows META-INF/container.xml to the package document, which
// is the only part of an EPUB whose location is fixed.
func epubPackagePath(reader *zip.Reader) string {
	file := zipEntry(reader, "META-INF/container.xml")
	if file == nil {
		return ""
	}
	body, err := zipEntryReader(file)
	if err != nil {
		return ""
	}
	defer func() { _ = body.Close() }()
	decoder := xml.NewDecoder(io.LimitReader(body, 64<<10))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "rootfile" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local == "full-path" {
				return attr.Value
			}
		}
	}
}

// readEPUBPackage returns the book's Dublin Core metadata and the href of the
// first document in the reading order.
func readEPUBPackage(reader *zip.Reader, opfPath string) (coreProperties, string) {
	file := zipEntry(reader, opfPath)
	if file == nil {
		return coreProperties{}, ""
	}
	body, err := zipEntryReader(file)
	if err != nil {
		return coreProperties{}, ""
	}
	defer func() { _ = body.Close() }()

	props := coreProperties{}
	manifest := map[string]string{}
	firstIDRef := ""
	field := ""
	decoder := xml.NewDecoder(io.LimitReader(body, 1<<20))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch element := token.(type) {
		case xml.StartElement:
			field = element.Name.Local
			switch element.Name.Local {
			case "item":
				id, href := "", ""
				for _, attr := range element.Attr {
					switch attr.Name.Local {
					case "id":
						id = attr.Value
					case "href":
						href = attr.Value
					}
				}
				if id != "" && href != "" {
					manifest[id] = href
				}
			case "itemref":
				if firstIDRef != "" {
					continue
				}
				for _, attr := range element.Attr {
					if attr.Name.Local == "idref" {
						firstIDRef = attr.Value
					}
				}
			}
		case xml.CharData:
			text := clean(string(element))
			if text == "" {
				continue
			}
			switch field {
			case "title":
				if props.Title == "" {
					props.Title = text
				}
			case "creator":
				props.Creator = text
			case "subject":
				props.Subject = joinDistinct(props.Subject, text)
			case "description":
				props.Description = truncateRunes(text, 240)
			}
		case xml.EndElement:
			field = ""
		}
	}
	href := manifest[firstIDRef]
	if href == "" {
		return props, ""
	}
	return props, path.Join(path.Dir(opfPath), href)
}

func joinDistinct(existing, value string) string {
	if existing == "" {
		return value
	}
	if strings.Contains(existing, value) {
		return existing
	}
	return existing + ", " + value
}
