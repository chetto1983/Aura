package filecard

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strings"
)

// zipEntry finds one part by name. OPC part names are case-sensitive in the
// spec and not always in the wild, so an exact match is tried first and a
// case-insensitive sweep second.
func zipEntry(reader *zip.Reader, name string) *zip.File {
	for _, file := range reader.File {
		if file.Name == name {
			return file
		}
	}
	for _, file := range reader.File {
		if strings.EqualFold(file.Name, name) {
			return file
		}
	}
	return nil
}

func zipEntryReader(file *zip.File) (io.ReadCloser, error) {
	return file.Open()
}

// notProse are elements whose character data is machinery, not text. Without
// this an EPUB's first "page" is the stylesheet its cover embeds.
var notProse = map[string]bool{"style": true, "script": true}

// xmlTextSegments streams an XML part and returns its character data grouped by
// the elements that end a run of text — paragraphs in a document, paragraphs in
// a slide. It reads at most limit bytes so a 200 MB body part cannot be pulled
// into memory by a card.
func xmlTextSegments(reader io.Reader, breakOn map[string]bool, limit int64) []string {
	decoder := xml.NewDecoder(io.LimitReader(reader, limit))
	var segments []string
	var current strings.Builder
	skipDepth := 0
	flush := func() {
		if text := clean(current.String()); text != "" {
			segments = append(segments, text)
		}
		current.Reset()
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			flush()
			return segments
		}
		switch element := token.(type) {
		case xml.StartElement:
			if skipDepth > 0 || notProse[element.Name.Local] {
				skipDepth++
			}
		case xml.CharData:
			if skipDepth == 0 {
				current.Write(element)
			}
		case xml.EndElement:
			if skipDepth > 0 {
				skipDepth--
				continue
			}
			if breakOn[element.Name.Local] {
				flush()
			}
		}
	}
}

// zipXMLSegments is xmlTextSegments over one zip part.
func zipXMLSegments(reader *zip.Reader, name string, breakOn map[string]bool) []string {
	file := zipEntry(reader, name)
	if file == nil {
		return nil
	}
	body, err := zipEntryReader(file)
	if err != nil {
		return nil
	}
	defer func() { _ = body.Close() }()
	return xmlTextSegments(body, breakOn, maxTextChars)
}

// coreProperties are the Dublin Core fields every OOXML package carries in
// docProps/core.xml. They are the one place a file states, in a human's words,
// what it is — and they are free to read: one small part, no body parsing.
type coreProperties struct {
	Title       string
	Subject     string
	Keywords    string
	Description string
	Creator     string
}

// describe renders the properties a person filled in, one line each. Empty
// fields are left out rather than printed empty: most files carry none of these.
func (p coreProperties) describe() []string {
	var out []string
	for _, field := range []struct{ label, value string }{
		{"Subject", p.Subject},
		{"Keywords", p.Keywords},
		{"Description", p.Description},
		{"Author", p.Creator},
	} {
		if value := clean(field.value); value != "" {
			out = append(out, field.label+": "+truncateRunes(value, 200))
		}
	}
	return out
}

func readCoreProperties(reader *zip.Reader) coreProperties {
	file := zipEntry(reader, "docProps/core.xml")
	if file == nil {
		return coreProperties{}
	}
	body, err := zipEntryReader(file)
	if err != nil {
		return coreProperties{}
	}
	defer func() { _ = body.Close() }()

	props := coreProperties{}
	decoder := xml.NewDecoder(io.LimitReader(body, 64<<10))
	field := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			return props
		}
		switch element := token.(type) {
		case xml.StartElement:
			field = element.Name.Local
		case xml.CharData:
			text := clean(string(element))
			if text == "" {
				continue
			}
			switch field {
			case "title":
				props.Title = text
			case "subject":
				props.Subject = text
			case "keywords":
				props.Keywords = text
			case "description":
				props.Description = text
			case "creator":
				props.Creator = text
			}
		case xml.EndElement:
			field = ""
		}
	}
}

func ooxmlTitle(reader *zip.Reader) string {
	return readCoreProperties(reader).Title
}

// firstWords keeps the opening words of a body of text. Prose is kept, not
// summarised: the first paragraphs of a document are what a person would read
// to decide whether it is the file they meant.
func firstWords(segments []string, limit int) []string {
	var out []string
	used := 0
	for _, segment := range segments {
		words := strings.Fields(segment)
		if len(words) == 0 {
			continue
		}
		if used+len(words) > limit {
			words = words[:limit-used]
			if len(words) > 0 {
				out = append(out, strings.Join(words, " "))
			}
			return out
		}
		out = append(out, strings.Join(words, " "))
		used += len(words)
		if used >= limit {
			return out
		}
	}
	return out
}
