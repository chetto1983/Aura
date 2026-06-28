package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	targetSize = 12 << 20
	chunkSize  = 64 << 10
	overlap    = 512
)

type chunk struct {
	SourceID    string
	DocumentID  string
	ContentHash string
	ChunkHash   string
	Index       int
	Count       int
	Start       int
	End         int
	Text        string
}

type ingestResult struct {
	SourceID    string
	DocumentID  string
	ContentHash string
	Added       int
	Skipped     int
	Total       int
	Duration    time.Duration
}

type store struct {
	docs   map[string]string
	chunks map[string]chunk
}

type check struct {
	Name   string
	Pass   bool
	Detail string
}

func main() {
	fmt.Println("[SPIKE] 044 memory ingest provenance/dedup contract")

	s := newStore()
	var checks []check

	docA := buildLargeMarkdown("alpha", targetSize, "Mario Rossi contract seed at the beginning.")
	start := time.Now()
	first := s.Ingest("telegram:chat-42", "telegram-file-alpha", docA)
	first.Duration = time.Since(start)
	checks = append(checks, check{"large fixture", len(docA) > 5<<20, fmt.Sprintf("size=%s chunks=%d", mib(len(docA)), first.Total)})
	checks = append(checks, check{"first ingest adds chunks", first.Added == first.Total && first.Total > 1, fmt.Sprintf("added=%d total=%d", first.Added, first.Total)})

	second := s.Ingest("telegram:chat-42", "telegram-file-alpha", docA)
	checks = append(checks, check{"same document re-ingest no-op", second.Added == 0 && second.Skipped == first.Total, fmt.Sprintf("added=%d skipped=%d", second.Added, second.Skipped)})

	sameContentDifferentDoc := s.Ingest("telegram:chat-42", "telegram-file-alpha-copy", docA)
	checks = append(checks, check{"same content different document preserved", sameContentDifferentDoc.Added == first.Total, fmt.Sprintf("added=%d original_hash=%s", sameContentDifferentDoc.Added, short(first.ContentHash))})

	docB := buildLargeMarkdown("bravo", 2<<20, "Mario Rossi appears in a separate imported PDF.")
	docC := buildLargeMarkdown("charlie", 2<<20, "Mario Rossi appears in a separate audit attachment.")
	_ = s.Ingest("telegram:chat-99", "separate-pdf", docB)
	_ = s.Ingest("github:issue-77", "audit-attachment", docC)
	occurrences := s.EntityOccurrences("Mario Rossi")
	checks = append(checks, check{"semantic cousin isolation", len(occurrences) >= 4, fmt.Sprintf("document_occurrences=%s", formatOccurrences(occurrences))})

	checks = append(checks, check{"metadata complete", s.MetadataComplete(), fmt.Sprintf("stored_chunks=%d", len(s.chunks))})
	checks = append(checks, check{"chunk offsets valid", s.OffsetsValid(), "all chunks have monotonic byte offsets and declared counts"})

	verdict := "VALIDATED"
	for _, c := range checks {
		if !c.Pass {
			verdict = "INVALIDATED"
		}
	}

	fmt.Println("[INGEST]")
	fmt.Printf("  first source=%s document=%s hash=%s added=%d duration=%s\n", first.SourceID, first.DocumentID, short(first.ContentHash), first.Added, first.Duration)
	fmt.Printf("  reingest source=%s document=%s added=%d skipped=%d\n", second.SourceID, second.DocumentID, second.Added, second.Skipped)
	fmt.Printf("  copy source=%s document=%s added=%d hash=%s\n", sameContentDifferentDoc.SourceID, sameContentDifferentDoc.DocumentID, sameContentDifferentDoc.Added, short(sameContentDifferentDoc.ContentHash))

	fmt.Println("[CHECKS]")
	for _, c := range checks {
		label := "PASS"
		if !c.Pass {
			label = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s\n", label, c.Name, c.Detail)
	}

	fmt.Printf("[SUMMARY] verdict=%s stored_chunks=%d\n", verdict, len(s.chunks))
	if verdict != "VALIDATED" {
		os.Exit(1)
	}
}

func newStore() *store {
	return &store{docs: map[string]string{}, chunks: map[string]chunk{}}
}

func (s *store) Ingest(sourceID, documentID, text string) ingestResult {
	contentHash := hash(text)
	docKey := sourceID + "\x00" + documentID
	chunks := split(sourceID, documentID, contentHash, text)
	if s.docs[docKey] == contentHash {
		return ingestResult{SourceID: sourceID, DocumentID: documentID, ContentHash: contentHash, Added: 0, Skipped: len(chunks), Total: len(chunks)}
	}
	s.docs[docKey] = contentHash
	added := 0
	for _, ch := range chunks {
		key := sourceID + "\x00" + documentID + "\x00" + contentHash + "\x00" + ch.ChunkHash
		if _, exists := s.chunks[key]; exists {
			continue
		}
		s.chunks[key] = ch
		added++
	}
	return ingestResult{SourceID: sourceID, DocumentID: documentID, ContentHash: contentHash, Added: added, Total: len(chunks)}
}

func split(sourceID, documentID, contentHash, text string) []chunk {
	if text == "" {
		return nil
	}
	var bounds [][2]int
	for start := 0; start < len(text); {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		bounds = append(bounds, [2]int{start, end})
		if end == len(text) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	out := make([]chunk, 0, len(bounds))
	for i, b := range bounds {
		body := text[b[0]:b[1]]
		out = append(out, chunk{
			SourceID:    sourceID,
			DocumentID:  documentID,
			ContentHash: contentHash,
			ChunkHash:   hash(fmt.Sprintf("%s:%s:%d:%s", sourceID, documentID, i, body)),
			Index:       i,
			Count:       len(bounds),
			Start:       b[0],
			End:         b[1],
			Text:        body,
		})
	}
	return out
}

func (s *store) MetadataComplete() bool {
	for _, ch := range s.chunks {
		if ch.SourceID == "" || ch.DocumentID == "" || ch.ContentHash == "" || ch.ChunkHash == "" || ch.Count <= 0 || ch.End <= ch.Start {
			return false
		}
	}
	return true
}

func (s *store) OffsetsValid() bool {
	byDoc := map[string][]chunk{}
	for _, ch := range s.chunks {
		key := ch.SourceID + "\x00" + ch.DocumentID + "\x00" + ch.ContentHash
		byDoc[key] = append(byDoc[key], ch)
	}
	for _, chunks := range byDoc {
		sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })
		for i, ch := range chunks {
			if ch.Index != i || ch.Count != len(chunks) || ch.Start < 0 || ch.End <= ch.Start {
				return false
			}
			if i > 0 && ch.Start >= chunks[i-1].End {
				return false
			}
		}
	}
	return true
}

func (s *store) EntityOccurrences(entity string) map[string]int {
	out := map[string]int{}
	for _, ch := range s.chunks {
		if strings.Contains(ch.Text, entity) {
			out[ch.SourceID+"/"+ch.DocumentID]++
		}
	}
	return out
}

func buildLargeMarkdown(label string, minBytes int, seed string) string {
	var b strings.Builder
	b.Grow(minBytes + 4096)
	b.WriteString("# Synthetic large document ")
	b.WriteString(label)
	b.WriteString("\n\n")
	b.WriteString(seed)
	b.WriteString("\n\n")
	i := 0
	for b.Len() < minBytes {
		fmt.Fprintf(&b, "## Section %05d\n", i)
		fmt.Fprintf(&b, "This section belongs to %s and carries repeatable content for deterministic chunking. ", label)
		fmt.Fprintf(&b, "Mario Rossi is mentioned only as content, never as a global dedup key. ")
		fmt.Fprintf(&b, "The provenance key is source_id plus document_id plus content_hash.\n\n")
		i++
	}
	b.WriteString("End marker for ")
	b.WriteString(label)
	b.WriteString(".\n")
	return b.String()
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func mib(size int) string {
	return fmt.Sprintf("%.2fMiB", float64(size)/(1<<20))
}

func formatOccurrences(in map[string]int) string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, in[key]))
	}
	return strings.Join(parts, ", ")
}
