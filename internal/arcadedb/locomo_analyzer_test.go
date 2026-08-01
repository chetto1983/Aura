//go:build arcadedb_integration

// Is the LOCOMO score low because full-text retrieval is the wrong tool, or
// because the index was analysing English with an Italian stemmer?
//
// The first run scored 18.6% R@10 against the shared Chunk index, which carries
// ItalianAnalyzer because the corpus it was built for is the Costituzione. LOCOMO
// is English. Italian stemming on English text is not a small mismatch: it rules
// out most of what a stemmer is for.
//
// So this loads the same turns into a type of their own with EnglishAnalyzer and
// scores identically. The difference between the two runs is the analyser and
// nothing else. Whatever is left after that gap is closed is the real limit of
// lexical-only retrieval on conversation.
//
// Run: go test -tags arcadedb_integration -run LocomoEnglish -v ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

const locomoEnglishType = "LocomoTurn"

func ensureEnglishLocomoSchema(t *testing.T, client *Client) {
	t.Helper()
	for _, ddl := range []string{
		"CREATE VERTEX TYPE " + locomoEnglishType + " IF NOT EXISTS",
		"CREATE PROPERTY " + locomoEnglishType + ".id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + locomoEnglishType + ".conversation IF NOT EXISTS STRING",
		"CREATE PROPERTY " + locomoEnglishType + ".dia_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + locomoEnglishType + ".text IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON " + locomoEnglishType + " (id) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + locomoEnglishType + " (text) FULL_TEXT " +
			"METADATA {analyzer:'org.apache.lucene.analysis.en.EnglishAnalyzer'}",
	} {
		if _, err := client.Command(context.Background(), ddl, nil); err != nil {
			t.Fatalf("ddl %q: %v", ddl, err)
		}
	}
}

const locomoEnglishWrite = `
UNWIND $turns AS turn
CREATE (c:LocomoTurn)
SET c.id = turn.id, c.conversation = turn.conversation,
    c.dia_id = turn.dia_id, c.text = turn.text
RETURN count(c) AS written
`

func TestLocomoEnglishAnalyzerRecall(t *testing.T) {
	client := documentClient(t)
	ctx := context.Background()
	samples := loadLocomo(t)
	ensureEnglishLocomoSchema(t, client)

	if _, err := client.Command(ctx, "DELETE FROM "+locomoEnglishType, nil); err != nil {
		t.Fatalf("clear %s: %v", locomoEnglishType, err)
	}
	started := time.Now()
	loaded := 0
	for index, sample := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		turns := turnsOf(sample)
		payload := make([]any, 0, len(turns))
		for _, turn := range turns {
			payload = append(payload, map[string]any{
				"id":           conversation + "#" + turn.DiaID,
				"conversation": conversation,
				"dia_id":       turn.DiaID,
				"text":         turn.Speaker + ": " + turn.Text,
			})
		}
		for start := 0; start < len(payload); start += locomoBatchSize {
			end := min(start+locomoBatchSize, len(payload))
			if _, err := client.Write(ctx, locomoEnglishWrite,
				map[string]any{"turns": payload[start:end]}); err != nil {
				t.Fatalf("write %s: %v", conversation, err)
			}
		}
		loaded += len(turns)
	}
	t.Logf("loaded %d turns under EnglishAnalyzer in %s",
		loaded, time.Since(started).Round(time.Millisecond))

	overall := &recallScore{}
	byCategory := map[int]*recallScore{}
	started = time.Now()
	searches := 0
	for index, sample := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		for _, qa := range sample.QA {
			if qa.Category == adversarialClass || len(qa.Evidence) == 0 {
				continue
			}
			rows, err := client.Query(ctx,
				"SELECT dia_id FROM "+locomoEnglishType+
					" WHERE SEARCH_INDEX('"+locomoEnglishType+"[text]', :q)"+
					" AND conversation = :c LIMIT 10",
				map[string]any{"q": escapeLucene(qa.Question), "c": conversation})
			if err != nil {
				rows = nil
			}
			searches++
			rank := -1
			for position, row := range rows {
				if slices.Contains(qa.Evidence, rowString(row, "dia_id")) {
					rank = position
					break
				}
			}
			overall.add(rank)
			if byCategory[qa.Category] == nil {
				byCategory[qa.Category] = &recallScore{}
			}
			byCategory[qa.Category].add(rank)
		}
	}

	elapsed := time.Since(started)
	t.Logf("%d questions scored in %s (%.1f ms/search)", searches,
		elapsed.Round(time.Second), float64(elapsed.Milliseconds())/float64(max(searches, 1)))
	t.Logf("%-14s %7s %8s %8s %8s", "category", "asked", "R@1", "R@5", "R@10")
	categories := []int{}
	for category := range byCategory {
		categories = append(categories, category)
	}
	sort.Ints(categories)
	for _, category := range categories {
		score := byCategory[category]
		t.Logf("%-14s %7d %7.1f%% %7.1f%% %7.1f%%", locomoCategories[category],
			score.asked, pct(score.hit1, score.asked),
			pct(score.hit5, score.asked), pct(score.hit10, score.asked))
	}
	t.Logf("%-14s %7d %7.1f%% %7.1f%% %7.1f%%", "OVERALL", overall.asked,
		pct(overall.hit1, overall.asked), pct(overall.hit5, overall.asked),
		pct(overall.hit10, overall.asked))

	// A few misses, printed, because an aggregate says how often and never why.
	shown := 0
	for index, sample := range samples {
		conversation := fmt.Sprintf("%s%d", locomoDocPrefix, index+1)
		for _, qa := range sample.QA {
			if shown >= 3 || qa.Category == adversarialClass || len(qa.Evidence) == 0 {
				continue
			}
			rows, err := client.Query(ctx,
				"SELECT dia_id, text FROM "+locomoEnglishType+
					" WHERE SEARCH_INDEX('"+locomoEnglishType+"[text]', :q)"+
					" AND conversation = :c LIMIT 3",
				map[string]any{"q": escapeLucene(qa.Question), "c": conversation})
			if err != nil || len(rows) == 0 {
				continue
			}
			if slices.Contains(qa.Evidence, rowString(rows[0], "dia_id")) {
				continue
			}
			shown++
			t.Logf("MISS  q=%q", qa.Question)
			t.Logf("      want dia %v", qa.Evidence)
			for _, row := range rows {
				t.Logf("      got  %-8s %s", rowString(row, "dia_id"),
					truncateRunes(strings.TrimSpace(rowString(row, "text")), 110))
			}
		}
	}
}
