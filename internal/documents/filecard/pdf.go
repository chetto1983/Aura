package filecard

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

// A PDF is the one common format whose words the card cannot reach.
//
// Its text lives in content streams that are Flate-compressed and written as
// glyph indices into a font; for the CID fonts every modern producer emits, a
// glyph index means nothing without that font's ToUnicode CMap. Extracting it is
// a PDF engine, not a card builder, so this reader takes only what a PDF states
// about itself in the clear: its metadata and its page count. When those are
// absent — and for the Normattiva decrees in the reference corpus they are, the
// producer wrote nothing but "iText" — the card says so instead of pretending.
const (
	pdfHeadBytes    = 1 << 20
	pdfTailBytes    = 256 << 10
	maxPDFScanBytes = 32 << 20
)

func buildPDF(req Request, card Card) (Card, error) {
	file, err := os.Open(req.Path)
	if err != nil {
		return card, fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, pdfHeadBytes)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return card, fmt.Errorf("read pdf: %w", err)
	}
	head = head[:n]
	tail := readTail(file, req.SizeBytes, pdfTailBytes)

	card.Title = pdfMetadata(head, tail, "Title", "dc:title")
	for _, field := range []struct{ label, info, xmp string }{
		{"Subject", "Subject", "dc:description"},
		{"Keywords", "Keywords", "pdf:Keywords"},
		{"Author", "Author", "dc:creator"},
	} {
		if value := pdfMetadata(head, tail, field.info, field.xmp); value != "" {
			card.Text = append(card.Text, field.label+": "+truncateRunes(value, 200))
		}
	}

	pages, exact := pdfPageCount(file, head)
	switch {
	case pages > 0 && exact:
		card.Facts = append(card.Facts, plural(pages, "page", "pages"))
	case pages > 0:
		card.Facts = append(card.Facts, fmt.Sprintf("at least %d pages", pages))
	}
	card.Caveats = append(card.Caveats,
		"Aura did not read this PDF's text at ingest — a PDF stores its words as font glyph indices, "+
			"which needs the file itself. Open it with document_open to read or search inside it.")
	if card.Title == "" && len(card.Text) == 0 {
		card.Caveats = append(card.Caveats,
			"It carries no title or subject of its own, so only its file name says what it is.")
	}
	return card, nil
}

func readTail(file *os.File, size int64, want int64) []byte {
	if size <= 0 || size <= pdfHeadBytes {
		return nil
	}
	offset := max(size-want, pdfHeadBytes)
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	tail, err := io.ReadAll(io.LimitReader(file, want))
	if err != nil {
		return nil
	}
	return tail
}

// pdfMetadata prefers the Info dictionary and falls back to XMP. Both are plain
// bytes near the head or the tail of the file, so neither costs a parse.
func pdfMetadata(head, tail []byte, infoKey, xmpKey string) string {
	for _, buffer := range [][]byte{head, tail} {
		if value := pdfInfoValue(buffer, infoKey); value != "" {
			return value
		}
	}
	for _, buffer := range [][]byte{head, tail} {
		if value := xmpValue(buffer, xmpKey); value != "" {
			return value
		}
	}
	return ""
}

// pdfInfoValue reads /Key (literal) or /Key <hex> out of the Info dictionary.
func pdfInfoValue(buffer []byte, key string) string {
	marker := []byte("/" + key)
	for offset := 0; ; {
		index := bytes.Index(buffer[offset:], marker)
		if index < 0 {
			return ""
		}
		cursor := offset + index + len(marker)
		// /Titled would match /Title; a real key is followed by a delimiter.
		if cursor < len(buffer) && isPDFNameByte(buffer[cursor]) {
			offset = cursor
			continue
		}
		for cursor < len(buffer) && (buffer[cursor] == ' ' || buffer[cursor] == '\r' || buffer[cursor] == '\n') {
			cursor++
		}
		if cursor >= len(buffer) {
			return ""
		}
		switch buffer[cursor] {
		case '(':
			return decodePDFText(readLiteralString(buffer[cursor+1:]))
		case '<':
			end := bytes.IndexByte(buffer[cursor:], '>')
			if end < 0 {
				return ""
			}
			return decodePDFText(decodeHexString(string(buffer[cursor+1 : cursor+end])))
		}
		offset = cursor
	}
}

func isPDFNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// readLiteralString reads a PDF literal string, honouring backslash escapes and
// nested parentheses.
func readLiteralString(buffer []byte) string {
	var out []byte
	depth := 1
	for i := 0; i < len(buffer) && len(out) < 1024; i++ {
		switch buffer[i] {
		case '\\':
			if i+1 < len(buffer) {
				i++
				out = append(out, buffer[i])
			}
		case '(':
			depth++
			out = append(out, '(')
		case ')':
			depth--
			if depth == 0 {
				return string(out)
			}
			out = append(out, ')')
		default:
			out = append(out, buffer[i])
		}
	}
	return string(out)
}

func decodeHexString(text string) string {
	var out []byte
	var high byte
	haveHigh := false
	for _, r := range text {
		value, ok := hexValue(byte(r))
		if !ok {
			continue
		}
		if !haveHigh {
			high, haveHigh = value, true
			continue
		}
		out = append(out, high<<4|value)
		haveHigh = false
	}
	return string(out)
}

func hexValue(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// decodePDFText folds the UTF-16BE form a PDF text string may use. Without this
// a title reads as "S i e m e n s", which is what the raw bytes look like.
func decodePDFText(text string) string {
	if len(text) >= 2 && text[0] == 0xFE && text[1] == 0xFF {
		units := make([]uint16, 0, (len(text)-2)/2)
		for i := 2; i+1 < len(text); i += 2 {
			units = append(units, uint16(text[i])<<8|uint16(text[i+1]))
		}
		return clean(string(utf16.Decode(units)))
	}
	return clean(text)
}

// xmpValue reads the first rdf:li (or direct text) under an XMP property.
func xmpValue(buffer []byte, key string) string {
	open := []byte("<" + key)
	start := bytes.Index(buffer, open)
	if start < 0 {
		return ""
	}
	end := bytes.Index(buffer[start:], []byte("</"+key+">"))
	if end < 0 {
		return ""
	}
	segment := string(buffer[start : start+end])
	if item := strings.LastIndex(segment, "<rdf:li"); item >= 0 {
		segment = segment[item:]
	}
	if gt := strings.Index(segment, ">"); gt >= 0 {
		segment = segment[gt+1:]
	}
	if lt := strings.Index(segment, "<"); lt >= 0 {
		segment = segment[:lt]
	}
	return clean(segment)
}

// pdfPageCount counts page objects, and reports whether the count can be
// trusted. A linearized file states its page count in the clear; otherwise the
// /Type /Page objects are counted, which under-reports when a producer packs
// them into compressed object streams — hence "at least".
func pdfPageCount(file *os.File, head []byte) (int, bool) {
	if pages := intAfter(head, "/N "); pages > 0 && bytes.Contains(head[:min(len(head), 2048)], []byte("/Linearized")) {
		return pages, true
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, false
	}
	reader := io.LimitReader(file, maxPDFScanBytes)
	buffer := make([]byte, 1<<20)
	carry := make([]byte, 0, 16)
	count := 0
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			chunk := append(carry, buffer[:n]...) //nolint:gocritic // carry is the previous chunk's tail, by design.
			count += countPageObjects(chunk)
			keep := min(len(chunk), 16)
			carry = append(carry[:0], chunk[len(chunk)-keep:]...)
		}
		if err != nil {
			break
		}
	}
	return count, count > 0
}

// countPageObjects counts "/Type /Page" without counting "/Type /Pages".
func countPageObjects(buffer []byte) int {
	marker := []byte("/Type")
	count := 0
	for offset := 0; ; {
		index := bytes.Index(buffer[offset:], marker)
		if index < 0 {
			return count
		}
		cursor := offset + index + len(marker)
		for cursor < len(buffer) && (buffer[cursor] == ' ' || buffer[cursor] == '\r' || buffer[cursor] == '\n') {
			cursor++
		}
		if bytes.HasPrefix(buffer[cursor:], []byte("/Page")) {
			after := cursor + len("/Page")
			if after >= len(buffer) || !isPDFNameByte(buffer[after]) {
				count++
			}
		}
		offset = cursor
	}
}

func intAfter(buffer []byte, marker string) int {
	index := bytes.Index(buffer, []byte(marker))
	if index < 0 {
		return 0
	}
	rest := buffer[index+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	value, err := strconv.Atoi(string(rest[:end]))
	if err != nil {
		return 0
	}
	return value
}
