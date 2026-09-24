//go:build arcadedb_integration

// Documents over a live ArcadeDB (spec §3, §8): a library with one vector in another space is
// served lexically -- grouped, floored, abstaining where it cannot answer -- and the dense
// legs never rank a vector from another space. An external test package, so the retriever in
// internal/documents can drive the real index without an import cycle.
//
// Run: arcade-vm.sh DocumentSpaceLive
package arcadedb_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/embeddings"
)

const (
	liveIdentity   = "20000000-0000-0000-0000-000000000009"
	liveSpace      = "es1-docs-live-a"
	liveOtherSpace = "es1-docs-live-b"
)

// liveQuery is the query vector: in-space transcripts sit near it, the other-space passage on it.
var liveQuery = []float64{1, 0, 0}

type oneTenant struct{ client *arcadedb.Client }

func (o oneTenant) For(context.Context, string) (*arcadedb.Client, error) { return o.client, nil }

type liveEmbedder struct{ space string }

func (e liveEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = append([]float64(nil), liveQuery...)
	}
	return out, nil
}

func (e liveEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func liveDocumentDatabase(t *testing.T) (*arcadedb.Client, func() *arcadedb.Client) {
	t.Helper()
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	user := os.Getenv("ARCADEDB_USER")
	if user == "" {
		user = "root"
	}
	password := os.Getenv("ARCADEDB_PASSWORD")
	ctx := context.Background()
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: "unused", User: user, Password: password})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	database := fmt.Sprintf("aura_documents_space_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(ctx, database); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop disposable database %s: %v", database, err)
		}
	})
	// A fresh client is a fresh gate cache: the test asks for one after changing a stamp.
	fresh := func() *arcadedb.Client {
		client, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: database, User: user, Password: password})
		if err != nil {
			t.Fatalf("disposable client: %v", err)
		}
		return client
	}
	client := fresh()
	for _, statement := range documentDDL(len(liveQuery)) {
		if _, err := client.Command(ctx, statement, nil); err != nil {
			t.Fatalf("DDL %q: %v", statement, err)
		}
	}
	return client, fresh
}

// documentDDL mirrors services/ingest/arcade.py _document_ddl, which owns the real schema: the
// properties the reads decode, the three full-text indexes with the analyzer pinned, both
// vector indexes, and the stamp with the null strategy plan 2 measured.
func documentDDL(dims int) []string {
	fullText := "FULL_TEXT METADATA {analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}"
	vector := fmt.Sprintf(`LSM_VECTOR METADATA { "dimensions": %d, "similarity": "COSINE", "quantization": "NONE" }`, dims)
	statements := []string{}
	for _, t := range []struct {
		name       string
		properties []string
		indexes    []string
	}{
		{"Passage", []string{
			"passage_key STRING", "search_document_id STRING", "source_kind STRING", "source_key STRING",
			"raw_sha256 STRING", "schema_version STRING", "ordinal LONG", "text STRING",
			"normalized_text_sha256 STRING", "heading_path LIST OF STRING", "char_start LONG", "char_end LONG",
			"embedding ARRAY_OF_FLOATS", "embed_space STRING",
		}, []string{"(passage_key) UNIQUE", "(search_document_id) NOTUNIQUE", "(text) " + fullText,
			"(embedding) " + vector, "(embed_space) NOTUNIQUE NULL_STRATEGY INDEX"}},
		{"IndexedDocument", []string{
			"search_document_id STRING", "source_kind STRING", "source_key STRING", "file_name STRING",
			"file_name_words STRING", "raw_sha256 STRING", "normalized_text_sha256 STRING", "size_bytes LONG",
			"passage_count LONG", "card STRING", "indexed_at DATETIME", "embedding ARRAY_OF_FLOATS", "embed_space STRING",
		}, []string{"(search_document_id) UNIQUE", "(card) " + fullText, "(file_name_words) " + fullText,
			"(embedding) " + vector, "(embed_space) NOTUNIQUE NULL_STRATEGY INDEX"}},
	} {
		statements = append(statements, "CREATE VERTEX TYPE "+t.name+" IF NOT EXISTS")
		for _, property := range t.properties {
			name, kind, _ := strings.Cut(property, " ")
			statements = append(statements, "CREATE PROPERTY "+t.name+"."+name+" IF NOT EXISTS "+kind)
		}
		for _, index := range t.indexes {
			statements = append(statements, "CREATE INDEX IF NOT EXISTS ON "+t.name+" "+index)
		}
	}
	return statements
}

type liveDocument struct {
	id, fileName, words, card, content string
	passages                           []livePassage
}

type livePassage struct {
	text   string
	vector []float64
	space  string
}

// liveLibrary is the VM library's shape in miniature: an English manual, an Italian
// transcript stored three times, an Italian screenshot, and the PRD, one of whose passages sits
// in another space and on the query's own vector.
func liveLibrary() []liveDocument {
	far := []float64{0, 1, 0}
	transcript := "In questo video parliamo di prompt engineering e di intelligenza artificiale: come si " +
		"scrive bene la domanda da porre al modello, con esempi e contesto."
	video := func(n int) liveDocument {
		return liveDocument{
			id: fmt.Sprintf("doc_video%d", n), fileName: "videoplayback.mp4", words: "videoplayback mp4",
			card: "videoplayback.mp4 — video, transcript in Italian about prompt engineering.", content: transcript,
			passages: []livePassage{{text: transcript, vector: []float64{0.9, 0.1, 0}, space: liveSpace}},
		}
	}
	return []liveDocument{
		{id: "doc_pid", fileName: "PidTemp_MultiZone_DOC_V11_en.pdf", words: "PidTemp MultiZone DOC V11 en pdf",
			card:    "PidTemp_MultiZone_DOC_V11_en.pdf — PDF manual. Titled: PID_Temp multi-zone temperature control.",
			content: "pid", passages: []livePassage{
				{text: "PID_Temp controls the temperature of several zones at once. Each zone has its own heating " +
					"and cooling actuator, and the controller keeps the zones coupled while it tunes them.", vector: far, space: liveSpace},
				{text: "The safety light curtain stops the actuator when a hand enters the protected area; restarting " +
					"requires an explicit acknowledgement on the panel.", vector: far, space: liveSpace},
			}},
		video(1), video(2), video(3),
		{id: "doc_clock", fileName: "orologio_impostazioni.png", words: "orologio impostazioni png",
			card:    "Screenshot delle impostazioni dell'orologio: fuso orario Europe/Rome, formato 24 ore.",
			content: "clock", passages: []livePassage{{text: "Impostazioni orologio. Fuso orario: Europe/Rome. " +
				"Formato 24 ore. Sincronizzazione automatica attiva.", vector: far, space: liveSpace}}},
		{id: "doc_prd", fileName: "prd.md", words: "prd md",
			card:    "prd.md — Markdown. Titled: Aura product requirements. Headings: Approvals and durable grants.",
			content: "prd", passages: []livePassage{
				{text: "Approvals and durable grants: an operator approves a tool once and the grant is durable " +
					"until revoked.", vector: far, space: liveSpace},
				{text: "Durable grants survive a restart; a revoked grant stops the next call.", vector: liveQuery, space: liveOtherSpace},
			}},
	}
}

func seedLiveLibrary(t *testing.T, client *arcadedb.Client, schema string) {
	t.Helper()
	ctx := context.Background()
	for _, doc := range liveLibrary() {
		if _, err := client.Command(ctx, "INSERT INTO IndexedDocument SET search_document_id = :id, "+
			"source_kind = 's3', source_key = :key, file_name = :name, file_name_words = :words, "+
			"raw_sha256 = :raw, normalized_text_sha256 = :normalized, size_bytes = 1000, passage_count = :count, "+
			"card = :card, embedding = :embedding, embed_space = :space", map[string]any{
			"id": doc.id, "key": "library/" + doc.id + "/" + doc.fileName, "name": doc.fileName, "words": doc.words,
			"raw": sha(doc.content), "normalized": sha(doc.content), "count": len(doc.passages), "card": doc.card,
			"embedding": []float64{0, 0, 1}, "space": liveSpace,
		}); err != nil {
			t.Fatalf("insert card %s: %v", doc.id, err)
		}
		for ordinal, passage := range doc.passages {
			if _, err := client.Command(ctx, "INSERT INTO Passage SET passage_key = :key, search_document_id = :id, "+
				"source_kind = 's3', source_key = :p_source, raw_sha256 = :p_raw, schema_version = :p_schema, "+
				"ordinal = :p_ordinal, `text` = :p_text, normalized_text_sha256 = :normalized, "+
				"embedding = :embedding, embed_space = :space", map[string]any{
				"key": fmt.Sprintf("%s:%d", doc.id, ordinal), "id": doc.id, "p_source": "library/" + doc.id + "/" + doc.fileName,
				"p_raw": sha(doc.content), "p_schema": schema, "p_ordinal": ordinal, "p_text": passage.text,
				"normalized": sha(passage.text), "embedding": passage.vector, "space": passage.space,
			}); err != nil {
				t.Fatalf("insert passage %s:%d: %v", doc.id, ordinal, err)
			}
		}
	}
}

func liveRetriever(t *testing.T, client *arcadedb.Client) (*documents.HostRetriever, *arcadedb.DocumentIndex) {
	t.Helper()
	index, err := arcadedb.NewDocumentIndex(oneTenant{client}, arcadedb.DocumentIndexConfig{Dimensions: len(liveQuery)})
	if err != nil {
		t.Fatalf("NewDocumentIndex: %v", err)
	}
	return &documents.HostRetriever{
		ControlPlane: &documents.ArcadeRetrievalControlPlane{Index: index},
		PassageIndex: index, Embedder: liveEmbedder{space: liveSpace},
	}, index
}

// The schema version the reads accept is document-v1:standard-analyzer:cosine:none:<dims>
// (document_schema.go schemaVersion); the seed must write exactly that.
const liveSchema = "document-v1:standard-analyzer:cosine:none:3"

func TestDocumentSpaceLiveLexicalModeAnswersAndAbstains(t *testing.T) {
	client, _ := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	retriever, index := liveRetriever(t, client)
	ctx := context.Background()
	if open, err := index.DocumentsDenseOpen(ctx, liveIdentity, liveSpace); err != nil || open {
		t.Fatalf("gate with a passage in another space: open=%v err=%v, want closed", open, err)
	}
	for _, test := range []struct {
		query, top string
	}{
		{"prompt engineering intelligenza artificiale", "videoplayback.mp4"},
		{"approvals and durable grants", "prd.md"},
		{"fuso orario Europe/Rome", "orologio_impostazioni.png"},
		{"heating and cooling actuator", "PidTemp_MultiZone_DOC_V11_en.pdf"},
		{"orari dei traghetti per la Sardegna", ""},
		{"recipe for chocolate cake", ""},
		{"chi è il?", ""},
	} {
		response, err := retriever.Retrieve(ctx, documents.RetrievalRequest{IdentityID: liveIdentity, Query: test.query})
		if err != nil {
			t.Fatalf("%q: %v", test.query, err)
		}
		if response.Status != documents.RetrievalLexicalOnly || response.DegradationReason != documents.DegradationSpaceMismatch {
			t.Fatalf("%q: status %q reason %q, want lexical_only for the space mismatch", test.query,
				response.Status, response.DegradationReason)
		}
		if test.top == "" {
			if !response.Abstained || len(response.Documents) != 0 {
				t.Fatalf("%q: %d documents, want abstention", test.query, len(response.Documents))
			}
			continue
		}
		if len(response.Documents) == 0 || response.Documents[0].Title != test.top {
			t.Fatalf("%q: documents %+v, want %s first", test.query, response.Documents, test.top)
		}
		videos := 0
		for _, doc := range response.Documents {
			if doc.Title == "videoplayback.mp4" {
				videos++
			}
		}
		if videos > 1 {
			t.Fatalf("%q: the three identical transcripts took %d places", test.query, videos)
		}
	}
}

// Review Focus 3: inside a cached "open" the other-space passage -- sitting on the query's own
// vector, the nearest possible neighbour -- must still never be ranked.
func TestDocumentSpaceLiveFusedLegNeverRanksAnotherSpace(t *testing.T) {
	client, _ := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	_, index := liveRetriever(t, client)
	candidates, err := index.FusedCandidates(context.Background(), arcadedb.FusedCandidateQuery{
		CandidateFilter: arcadedb.CandidateFilter{IdentityID: liveIdentity, Limit: 10},
		Query:           "durable grants prompt engineering", Embedding: liveQuery, Space: liveSpace,
	})
	if err != nil {
		t.Fatalf("FusedCandidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.PassageID == "doc_prd:1" {
			t.Fatalf("the passage stamped %s was ranked for a query in %s", liveOtherSpace, liveSpace)
		}
	}
	if len(candidates) == 0 || !strings.HasPrefix(candidates[0].SearchDocumentID, "doc_video") {
		t.Fatalf("candidates = %+v, want the in-space transcript first", candidates)
	}
}

// The cockpit's report runs on 26.9.1 as written: the four counts per type, a memory whose
// types do not exist yet counted as empty, the stuck file named, and sum(`text`.length()).
func TestDocumentSpaceLiveReportAndWorkParse(t *testing.T) {
	client, _ := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	ctx := context.Background()
	// CocoIndex writes a file's card and passages together, so the prd's card shares the
	// other-space stamp its passage carries.
	if _, err := client.Command(ctx, "UPDATE IndexedDocument SET embed_space = :space WHERE search_document_id = 'doc_prd'",
		map[string]any{"space": liveOtherSpace}); err != nil {
		t.Fatalf("restamp card: %v", err)
	}
	report, err := client.SpaceReport(ctx, liveIdentity, "es1-mem-live", liveSpace)
	if err != nil {
		t.Fatalf("SpaceReport: %v", err)
	}
	memory, docs := report.Families[0], report.Families[1]
	if !memory.Open || memory.Types[0] != (arcadedb.TypeTally{Type: "FACT"}) {
		t.Fatalf("memory = %+v, want an empty open family", memory)
	}
	wantPassages := arcadedb.TypeTally{Type: "Passage", InSpace: 7, OtherSpace: 1}
	if docs.Open || docs.Types[0] != wantPassages || docs.Types[1] != (arcadedb.TypeTally{Type: "IndexedDocument", InSpace: 5, OtherSpace: 1}) {
		t.Fatalf("documents = %+v, want 7+1 passages and 5+1 cards, closed", docs)
	}
	if len(report.StuckDocuments) != 1 || report.StuckDocuments[0].FileName != "prd.md" ||
		report.StuckDocuments[0].Space != liveOtherSpace {
		t.Fatalf("stuck = %+v, want prd.md in %s", report.StuckDocuments, liveOtherSpace)
	}
	work, err := client.CorpusWork(ctx, "es1-mem-live", liveOtherSpace, 100)
	if err != nil {
		t.Fatalf("CorpusWork: %v", err)
	}
	rows, chars, over := 0, 0, 0
	for _, doc := range liveLibrary() {
		for _, passage := range doc.passages {
			length := utf8.RuneCountInString(passage.text)
			if passage.space != liveOtherSpace {
				rows, chars = rows+1, chars+length
			}
			if length > 100 {
				over++
			}
		}
	}
	if work.Types[3] != (arcadedb.TypeWork{Type: "Passage", Rows: rows, Chars: chars}) || work.Types[4].Rows != 5 ||
		work.PassagesOverLimit != over {
		t.Fatalf("work = %+v, want Passage %d rows %d chars, 5 cards, %d over the limit", work, rows, chars, over)
	}
}

// Once every vector is in the reader's space the gate opens, and the dense path the engine
// runs with the space predicate is the complete one.
func TestDocumentSpaceLiveGateOpensOnceTheLibraryIsWhole(t *testing.T) {
	client, fresh := liveDocumentDatabase(t)
	seedLiveLibrary(t, client, liveSchema)
	if _, err := client.Command(context.Background(),
		"UPDATE Passage SET embed_space = :space WHERE passage_key = 'doc_prd:1'", map[string]any{"space": liveSpace}); err != nil {
		t.Fatalf("restamp: %v", err)
	}
	retriever, _ := liveRetriever(t, fresh())
	response, err := retriever.Retrieve(context.Background(), documents.RetrievalRequest{
		IdentityID: liveIdentity, Query: "prompt engineering intelligenza artificiale",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if response.Status != documents.RetrievalComplete || response.FloorsReason != arcadedb.ReasonUncalibratedFloors ||
		len(response.Documents) == 0 {
		t.Fatalf("response = %+v, want a complete dense answer naming its uncalibrated floors", response)
	}
}
