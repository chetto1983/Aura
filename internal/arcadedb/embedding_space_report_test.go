package arcadedb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// reportAnswer answers the report's statements by what they ask: the type after FROM and which
// of the four counts it is. counts maps "<type> <kind>" to n; anything else counts zero.
func reportAnswer(counts map[string]int, other func(statement string) (testResponse, bool)) func(recordedRequest) testResponse {
	return func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if other != nil {
			if response, ok := other(statement); ok {
				return response
			}
		}
		typeName := strings.Fields(statement[strings.Index(statement, " FROM ")+len(" FROM "):])[0]
		kind := "none"
		switch {
		case strings.Contains(statement, "embedding IS NOT NULL AND embed_space = :space"):
			kind = "in"
		case strings.Contains(statement, "embedding IS NOT NULL AND "+otherSpace):
			kind = "other"
		case strings.Contains(statement, "embedding IS NULL AND embed_space = :space"):
			kind = "rejected"
		}
		return testResponse{Body: resultBody([]any{map[string]any{"n": counts[typeName+" "+kind]}})}
	}
}

func TestSpaceReportTalliesEachTypeAndClosesTheFamilyThatHasAVectorElsewhere(t *testing.T) {
	client, _ := routedClient(t, reportAnswer(map[string]int{
		"FACT in": 5, "FACT other": 2, "FACT none": 1, "FACT rejected": 1, "ConversationTurn in": 4,
	}, func(statement string) (testResponse, bool) {
		switch {
		case strings.Contains(statement, " FROM Passage "):
			return testResponse{Status: 500, Body: missingPassageBody}, true
		case strings.Contains(statement, " FROM IndexedDocument "):
			return testResponse{Status: 500, Body: missingTypeBody}, true
		case strings.Contains(statement, " FROM IngestStatus "):
			return testResponse{Status: 500, Body: strings.ReplaceAll(missingTypeBody, "IndexedDocument", "IngestStatus")}, true
		}
		return testResponse{}, false
	}))
	report, err := client.SpaceReport(t.Context(), tenantA, "es1-mem", "es1-docs")
	if err != nil {
		t.Fatalf("SpaceReport: %v", err)
	}
	if report.IdentityID != tenantA || len(report.Families) != 2 {
		t.Fatalf("report = %+v", report)
	}
	memory, documents := report.Families[0], report.Families[1]
	if memory.Family != "memory" || memory.Space != "es1-mem" || memory.Open || len(memory.Types) != 3 {
		t.Fatalf("memory = %+v, want the closed memory family over three types", memory)
	}
	if fact := memory.Types[0]; fact != (TypeTally{Type: "FACT", InSpace: 5, OtherSpace: 2, NoVector: 1, Rejected: 1}) {
		t.Fatalf("FACT tally = %+v", fact)
	}
	if turn := memory.Types[1]; turn != (TypeTally{Type: "ConversationTurn", InSpace: 4}) {
		t.Fatalf("turn tally = %+v", turn)
	}
	// Review Focus 2: a tenant with no document yet has an empty, open documents family.
	if documents.Family != "documents" || documents.Space != "es1-docs" || !documents.Open ||
		documents.Types[0] != (TypeTally{Type: "Passage"}) || len(report.StuckDocuments) != 0 || report.IngestStatus != "" {
		t.Fatalf("documents = %+v, stuck %v, ingest %q", documents, report.StuckDocuments, report.IngestStatus)
	}
}

func TestSpaceReportNamesTheDocumentsStuckInAnotherSpace(t *testing.T) {
	client, _ := routedClient(t, reportAnswer(map[string]int{"Passage other": 3, "IndexedDocument other": 1},
		func(statement string) (testResponse, bool) {
			switch {
			case strings.HasPrefix(statement, "SELECT file_name"):
				return testResponse{Body: resultBody([]any{
					map[string]any{"file_name": "scan.png", "source_key": "library/a/scan.png", "embed_space": "es1-old"},
					map[string]any{"file_name": "notes.txt", "source_key": "library/b/notes.txt"},
				})}, true
			case strings.Contains(statement, " FROM IngestStatus "):
				return testResponse{Body: resultBody([]any{map[string]any{"status": "ready", "errors": 2}})}, true
			}
			return testResponse{}, false
		}))
	report, err := client.SpaceReport(t.Context(), tenantA, "es1-mem", "es1-docs")
	if err != nil {
		t.Fatalf("SpaceReport: %v", err)
	}
	if report.Families[1].Open || report.IngestStatus != "ready" || report.IngestErrors != 2 {
		t.Fatalf("report = %+v, want a closed documents family and the ingest row", report)
	}
	want := []StuckDocument{
		{FileName: "scan.png", SourceKey: "library/a/scan.png", Space: "es1-old"},
		{FileName: "notes.txt", SourceKey: "library/b/notes.txt"},
	}
	if len(report.StuckDocuments) != 2 || report.StuckDocuments[0] != want[0] || report.StuckDocuments[1] != want[1] {
		t.Fatalf("stuck = %+v, want %+v", report.StuckDocuments, want)
	}
}

func TestCorpusWorkCountsWhatIsOutsideEachFamilysTarget(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, ".length() > :limit") {
			return testResponse{Body: resultBody([]any{map[string]any{"n": 7}})}
		}
		return testResponse{Body: resultBody([]any{map[string]any{"n": 2, "chars": 90}})}
	})
	work, err := client.CorpusWork(t.Context(), "es1-mem-new", "es1-docs-new", 2048)
	if err != nil {
		t.Fatalf("CorpusWork: %v", err)
	}
	if len(work.Types) != 5 || work.Types[0] != (TypeWork{Type: "FACT", Rows: 2, Chars: 90}) || work.PassagesOverLimit != 7 {
		t.Fatalf("work = %+v", work)
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		switch {
		case strings.Contains(statement, ".length() > :limit"):
			if params["limit"] != float64(2048) {
				t.Fatalf("over-limit params = %v", params)
			}
		case strings.Contains(statement, " FROM Passage "), strings.Contains(statement, " FROM IndexedDocument "):
			if params["space"] != "es1-docs-new" {
				t.Fatalf("a document type was measured against %v", params["space"])
			}
		default:
			if params["space"] != "es1-mem-new" || !strings.Contains(statement, "sum(") {
				t.Fatalf("a memory type was measured against %v:\n%s", params["space"], statement)
			}
		}
	}
}

func TestCorpusWorkSkipsTheOverLimitCountWithoutALimit(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: resultBody([]any{})}
	})
	work, err := client.CorpusWork(t.Context(), "es1-mem", "es1-docs", 0)
	if err != nil || work.PassagesOverLimit != 0 {
		t.Fatalf("work = %+v err = %v", work, err)
	}
	for _, request := range *requests {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, ":limit") {
			t.Fatalf("asked the over-limit count without a limit:\n%s", statement)
		}
	}
}

// reportTenantServer provisions the databases whose tenant user provisioned accepts and answers
// every query with row (none when row is empty). It returns the URL and the databases queried.
func reportTenantServer(t *testing.T, row string, provisioned func(database string) bool) (string, *[]string) {
	t.Helper()
	var mu sync.Mutex
	queried := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/ready") {
			user, _, _ := r.BasicAuth()
			if provisioned("mem_" + strings.TrimPrefix(user, "u_")) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		queried = append(queried, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":[`+row+`]}`)
	}))
	t.Cleanup(server.Close)
	return server.URL, &queried
}

func TestTenantBackfillSpaceReportsCoverEveryProvisionedTenant(t *testing.T) {
	url, queried := reportTenantServer(t, "", func(database string) bool { return database == databaseA })
	backfill := NewTenantBackfill(staticRoster{ids: []string{tenantB, tenantA}}, Config{BaseURL: url}, testCredentials(t), nil)
	reports, err := backfill.SpaceReports(t.Context(), "es1-mem", "es1-docs")
	if err != nil {
		t.Fatalf("SpaceReports: %v", err)
	}
	if len(reports) != 1 || reports[0].IdentityID != tenantA || slices.Contains(*queried, databaseB) {
		t.Fatalf("reports = %+v, queried %v: want tenant A alone (B has no memory)", reports, *queried)
	}
}

func TestTenantBackfillCorpusWorkSumsEveryTenant(t *testing.T) {
	url, _ := reportTenantServer(t, `{"n":2,"chars":30}`, func(string) bool { return true })
	backfill := NewTenantBackfill(staticRoster{ids: []string{tenantA, tenantB}}, Config{BaseURL: url}, testCredentials(t), nil)
	work, err := backfill.CorpusWork(t.Context(), "es1-mem", "es1-docs", 100)
	if err != nil {
		t.Fatalf("CorpusWork: %v", err)
	}
	if work.Types[0] != (TypeWork{Type: "FACT", Rows: 4, Chars: 60}) || work.PassagesOverLimit != 4 {
		t.Fatalf("work = %+v, want two tenants' answers summed", work)
	}
}

func TestTenantBackfillReportFailsOnlyWhenNoTenantAnswered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/ready") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom","detail":"disk","exception":"java.io.IOException"}`)
	}))
	t.Cleanup(server.Close)
	backfill := NewTenantBackfill(staticRoster{ids: []string{tenantA}}, Config{BaseURL: server.URL}, testCredentials(t), nil)
	if _, err := backfill.SpaceReports(t.Context(), "es1-mem", "es1-docs"); err == nil {
		t.Fatal("SpaceReports hid a failure of every tenant")
	}
	var unwired *TenantBackfill
	if _, err := unwired.CorpusWork(t.Context(), "a", "b", 0); err == nil {
		t.Fatal("CorpusWork ran without a wiring")
	}
}
