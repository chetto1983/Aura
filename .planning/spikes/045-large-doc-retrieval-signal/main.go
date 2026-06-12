package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	docTarget = 10 << 20
	chunkSize = 48 << 10
	overlap   = 768
)

type chunk struct {
	SourceID   string
	DocumentID string
	Index      int
	Count      int
	Start      int
	End        int
	Text       string
	Lower      string
}

type hit struct {
	Chunk chunk
	Score int
}

type check struct {
	Name   string
	Pass   bool
	Detail string
}

func main() {
	fmt.Println("[SPIKE] 045 large-document retrieval signal")

	seeds := []string{
		"aurastartfact alpha invoice approval is owned by channel intake.",
		"auramiddlefact bravo retention policy requires document provenance.",
		"auraendfact charlie final appendix contains the escalation contact.",
	}
	doc := buildDocument(seeds)
	chunks := split("telegram:chat-42", "large-audit-doc.md", doc)

	var checks []check
	checks = append(checks, check{"large fixture", len(doc) > 5<<20, fmt.Sprintf("size=%s chunks=%d", mib(len(doc)), len(chunks))})

	queries := []struct {
		name  string
		query string
		want  string
	}{
		{"beginning fact", "aurastartfact invoice approval", seeds[0]},
		{"middle fact", "auramiddlefact retention provenance", seeds[1]},
		{"end fact", "auraendfact appendix escalation", seeds[2]},
	}
	for _, q := range queries {
		results := search(chunks, q.query)
		pass := len(results) > 0 && strings.Contains(results[0].Chunk.Text, q.want)
		detail := "no hits"
		if len(results) > 0 {
			top := results[0].Chunk
			detail = fmt.Sprintf("top_chunk=%d/%d bytes=%d-%d score=%d", top.Index+1, top.Count, top.Start, top.End, results[0].Score)
		}
		checks = append(checks, check{q.name, pass, detail})
	}

	checks = append(checks, check{"citation metadata preserved", citationMetadata(chunks), "all chunks carry source, document, index/count, and byte offsets"})

	latencies := make([]time.Duration, 0, 600)
	for i := 0; i < 200; i++ {
		for _, q := range queries {
			start := time.Now()
			_ = search(chunks, q.query)
			latencies = append(latencies, time.Since(start))
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[int(float64(len(latencies))*0.95)]
	checks = append(checks, check{"local retrieval p95", p95 < 25*time.Millisecond, fmt.Sprintf("p95=%s runs=%d", p95, len(latencies))})

	verdict := "PARTIAL"
	for _, c := range checks {
		if !c.Pass {
			verdict = "INVALIDATED"
		}
	}

	fmt.Println("[CHECKS]")
	for _, c := range checks {
		label := "PASS"
		if !c.Pass {
			label = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s\n", label, c.Name, c.Detail)
	}

	fmt.Printf("[SUMMARY] verdict=%s caveat=local_keyword_harness_not_live_vector_mcp chunks=%d\n", verdict, len(chunks))
	if verdict == "INVALIDATED" {
		os.Exit(1)
	}
}

func buildDocument(seeds []string) string {
	var b strings.Builder
	b.Grow(docTarget + 4096)
	b.WriteString("# Large audit document\n\n")
	b.WriteString(seeds[0])
	b.WriteString("\n\n")
	writeFiller(&b, "start-band", docTarget/2)
	b.WriteString("\n\n")
	b.WriteString(seeds[1])
	b.WriteString("\n\n")
	writeFiller(&b, "middle-band", docTarget-4096)
	b.WriteString("\n\n")
	b.WriteString(seeds[2])
	b.WriteString("\n")
	return b.String()
}

func writeFiller(b *strings.Builder, label string, until int) {
	i := 0
	for b.Len() < until {
		fmt.Fprintf(b, "Section %s %05d: repeated neutral content keeps the document large without adding seeded answer tokens. ", label, i)
		fmt.Fprintf(b, "Chunking must retain byte offsets and enough overlap for citation recovery.\n")
		i++
	}
}

func split(sourceID, documentID, text string) []chunk {
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
		out = append(out, chunk{
			SourceID:   sourceID,
			DocumentID: documentID,
			Index:      i,
			Count:      len(bounds),
			Start:      b[0],
			End:        b[1],
			Text:       text[b[0]:b[1]],
			Lower:      strings.ToLower(text[b[0]:b[1]]),
		})
	}
	return out
}

func search(chunks []chunk, query string) []hit {
	terms := tokenize(query)
	out := make([]hit, 0)
	for _, ch := range chunks {
		score := 0
		for _, term := range terms {
			if strings.Contains(ch.Lower, term) {
				score++
			}
		}
		if score > 0 {
			out = append(out, hit{Chunk: ch, Score: score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Chunk.Index < out[j].Chunk.Index
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func citationMetadata(chunks []chunk) bool {
	for _, ch := range chunks {
		if ch.SourceID == "" || ch.DocumentID == "" || ch.Count != len(chunks) || ch.Start < 0 || ch.End <= ch.Start {
			return false
		}
	}
	return true
}

func mib(size int) string {
	return fmt.Sprintf("%.2fMiB", float64(size)/(1<<20))
}
