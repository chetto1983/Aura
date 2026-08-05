package documents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

const serviceTestIdentity = "00000000-0000-0000-0000-000000000001"

func TestServiceRegistersAnOwnedCandidateWithoutPublishingIt(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	service := &Service{Jobs: newFakeJobStore()}

	job, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli", IdentityID: serviceTestIdentity}, path)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobAccepted {
		t.Fatalf("status = %s, want %s", job.Status, JobAccepted)
	}
	if job.DocumentID == "" || !strings.HasPrefix(job.DocumentID, "doc_") {
		t.Fatalf("document id = %q, want the content-addressed search id", job.DocumentID)
	}
	// Nothing extracts, so nothing can claim a chunk count. A non-zero one here
	// would be the old pipeline's number surviving its own deletion.
	if job.SparseChunks != 0 || job.EmbeddedChunks != 0 {
		t.Fatalf("job = %#v, want no chunk counts", job)
	}
}

func TestServiceCatalogsAnOwnedIngestAsProcessing(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	catalog := &fakeIngestCatalog{}
	service := &Service{Jobs: newFakeJobStore(), Catalog: catalog}

	job, err := service.IngestPath(t.Context(), IngestRequest{
		SourceID: "cli", IdentityID: serviceTestIdentity,
	}, path)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.created.IdentityID != serviceTestIdentity {
		t.Fatalf("catalog identity = %q", catalog.created.IdentityID)
	}
	if catalog.created.Status != DocumentStatusStored {
		t.Fatalf("catalog status = %q, want stored", catalog.created.Status)
	}
	if catalog.created.Title != "manual.pdf" {
		t.Fatalf("catalog title = %q, want the file name", catalog.created.Title)
	}
	if catalog.created.Metadata["search_document_id"] != job.DocumentID ||
		catalog.created.Metadata["document_job_id"] != job.ID {
		t.Fatalf("catalog provenance = %#v", catalog.created.Metadata)
	}
}

// The whole point of the card: a file is findable the moment it lands, with no
// agent having opened it. Before this, ingest wrote a title and nothing else, so
// document_search could only find documents somebody had already found.
func TestServiceCardsAnIngestedFileWithoutAnAgentReadingIt(t *testing.T) {
	path := writeNamedTempFile(t, "clienti.csv", "Area;Localita\nNORD;TORINO\nNORD;TORINO\nSUD;GENOVA\n")
	catalog := &fakeIngestCatalog{}
	service := &Service{Jobs: newFakeJobStore(), Catalog: catalog}

	if _, err := service.IngestPath(t.Context(), IngestRequest{
		SourceID: "cli", IdentityID: "00000000-0000-0000-0000-000000000001",
	}, path); err != nil {
		t.Fatal(err)
	}
	if catalog.cardTarget != "catalog-1" {
		t.Fatalf("card written to %q, want the row ingest just created", catalog.cardTarget)
	}
	for _, want := range []string{"clienti.csv", "Area, Localita", "TORINO"} {
		if !strings.Contains(catalog.card, want) {
			t.Fatalf("card is missing %q:\n%s", want, catalog.card)
		}
	}
}

// A card is how a document is FOUND, not whether it was stored. An unreadable
// file, or a catalog that refuses the card, must not fail the upload — but it
// must not be silent either.
func TestServiceKeepsTheIngestWhenTheCardCannotBeStored(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	catalog := &fakeIngestCatalog{setCardErr: errors.New("card column unavailable")}
	service := &Service{Jobs: newFakeJobStore(), Catalog: catalog}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	job, err := service.IngestPath(t.Context(), IngestRequest{
		SourceID: "cli", IdentityID: "00000000-0000-0000-0000-000000000001",
	}, path)
	if err != nil {
		t.Fatalf("IngestPath error = %v, want the upload to survive a card failure", err)
	}
	if job.Status != JobAccepted {
		t.Fatalf("status = %s, want %s", job.Status, JobAccepted)
	}
	if !strings.Contains(logs.String(), "could not store the document card") {
		t.Fatalf("the card failure passed silently: %q", logs.String())
	}
}

func TestServiceRejectsAnIngestWithoutAnIdentity(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	catalog := &fakeIngestCatalog{}
	service := &Service{Jobs: newFakeJobStore(), Catalog: catalog}

	if _, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli"}, path); err == nil ||
		!strings.Contains(err.Error(), "identity") {
		t.Fatalf("error = %v, want identity refusal", err)
	}
	if catalog.created.Title != "" {
		t.Fatalf("catalog write happened without an owner: %#v", catalog.created)
	}
}

func TestServiceFailsTheJobWhenTheCatalogWriteFails(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	service := &Service{
		Jobs:    newFakeJobStore(),
		Catalog: &fakeIngestCatalog{err: errors.New("catalog unavailable")},
	}

	job, err := service.IngestPath(t.Context(), IngestRequest{
		SourceID: "cli", IdentityID: serviceTestIdentity,
	}, path)
	if err == nil || !strings.Contains(err.Error(), "catalog document") {
		t.Fatalf("IngestPath error = %v, want catalog failure", err)
	}
	if job == nil || job.Status != JobFailed {
		t.Fatalf("job = %#v, want failed ledger state", job)
	}
}

func TestServiceDoesNotPublishThroughTheLegacyProgressUpdate(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	jobs := newFakeJobStore()
	jobs.progressErr = errors.New("ledger unavailable")
	catalog := &fakeIngestCatalog{}
	service := &Service{Jobs: jobs, Catalog: catalog}

	job, err := service.IngestPath(t.Context(), IngestRequest{
		SourceID: "cli", IdentityID: serviceTestIdentity,
	}, path)
	if err != nil || job.Status != JobAccepted {
		t.Fatalf("candidate = %#v, error = %v", job, err)
	}
	if catalog.failedStatus != "" {
		t.Fatalf("candidate was incorrectly marked failed: %#v", catalog)
	}
}

func TestServiceRefusesFilesOverConfiguredLimit(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	service := &Service{Jobs: newFakeJobStore(), MaxBytes: 1}
	_, err := service.IngestPath(t.Context(), IngestRequest{}, path)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("want ErrFileTooLarge, got %v", err)
	}
}

func TestServiceReusesExistingJobForSameSourceDocumentHash(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	jobs := newFakeJobStore()
	service := &Service{Jobs: jobs}
	first, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli", IdentityID: serviceTestIdentity}, path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.IngestPath(t.Context(), IngestRequest{SourceID: "cli", IdentityID: serviceTestIdentity}, path)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("job ids should be reused: %q != %q", first.ID, second.ID)
	}
	if jobs.created != 2 {
		t.Fatalf("create calls = %d", jobs.created)
	}
}

func TestServiceGetsAndListsJobs(t *testing.T) {
	jobs := newFakeJobStore()
	job, err := jobs.Create(t.Context(), CreateJobParams{
		SourceID:    "cli",
		SourceKind:  "local",
		DocumentID:  "doc_1",
		ContentHash: "hash",
		FileName:    "manual.pdf",
		Status:      JobSearchable,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Jobs: jobs}
	got, err := service.GetJob(t.Context(), job.ID)
	if err != nil || got.ID != job.ID {
		t.Fatalf("GetJob = (%#v, %v)", got, err)
	}
	list, err := service.ListJobs(t.Context(), 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListJobs = (%#v, %v)", list, err)
	}
}

func TestServiceMissingDependencies(t *testing.T) {
	if _, err := (&Service{}).GetJob(t.Context(), "job-1"); err == nil {
		t.Fatal("GetJob without store: want error")
	}
	if _, err := (&Service{}).ListJobs(t.Context(), 1); err == nil {
		t.Fatal("ListJobs without store: want error")
	}
}

func TestServiceRejectsUnsupportedAndDirectoryPaths(t *testing.T) {
	service := &Service{Jobs: newFakeJobStore()}
	if _, err := service.IngestPath(t.Context(), IngestRequest{}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("directory path error = %v", err)
	}
	// .txt is now in the supportedDocumentExt allowlist; use a genuinely
	// unsupported extension to exercise the rejection path.
	blob := writeNamedTempFile(t, "notes.exe", "payload")
	if _, err := service.IngestPath(t.Context(), IngestRequest{
		SourceID: "cli", IdentityID: serviceTestIdentity,
	}, blob); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported path error = %v", err)
	}
}

func writeNamedTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + string(os.PathSeparator) + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type fakeIngestCatalog struct {
	created        CreateDocumentRequest
	err            error
	setStatusErr   error
	setCardErr     error
	card           string
	cardTarget     string
	failedStatus   DocumentStatus
	failedReason   string
	failedIdentity string
}

func (f *fakeIngestCatalog) CreateDocument(_ context.Context, req CreateDocumentRequest) (Document, error) {
	f.created = req
	return Document{ID: "catalog-1"}, f.err
}

func (f *fakeIngestCatalog) SetCard(_ context.Context, _, documentID, card string) error {
	f.cardTarget = documentID
	f.card = card
	return f.setCardErr
}

func (f *fakeIngestCatalog) SetSearchDocumentStatus(
	_ context.Context, identityID, _ string, status DocumentStatus, reason string,
) error {
	f.failedIdentity = identityID
	f.failedStatus = status
	f.failedReason = reason
	return f.setStatusErr
}

type fakeJobStore struct {
	byID        map[string]Job
	byKey       map[string]string
	next        int
	created     int
	progressErr error
}

func newFakeJobStore() *fakeJobStore {
	return &fakeJobStore{byID: map[string]Job{}, byKey: map[string]string{}}
}

func (f *fakeJobStore) Create(_ context.Context, params CreateJobParams) (Job, error) {
	f.created++
	key := params.SourceID + "|" + params.DocumentID + "|" + params.ContentHash
	if id := f.byKey[key]; id != "" {
		job := f.byID[id]
		job.OriginalPath = params.OriginalPath
		job.FileName = params.FileName
		job.MIMEType = params.MIMEType
		job.SizeBytes = params.SizeBytes
		f.byID[id] = job
		return job, nil
	}
	f.next++
	id := fmt.Sprintf("job-%d", f.next)
	job := Job{
		ID:           id,
		SourceID:     params.SourceID,
		SourceKind:   params.SourceKind,
		DocumentID:   params.DocumentID,
		ContentHash:  params.ContentHash,
		OriginalPath: params.OriginalPath,
		FileName:     params.FileName,
		MIMEType:     params.MIMEType,
		SizeBytes:    params.SizeBytes,
		Status:       params.Status,
	}
	f.byID[id] = job
	f.byKey[key] = id
	return job, nil
}

func (f *fakeJobStore) Get(_ context.Context, id string) (Job, error) {
	job, ok := f.byID[id]
	if !ok {
		return Job{}, errors.New("not found")
	}
	return job, nil
}

func (f *fakeJobStore) GetByDocumentID(_ context.Context, documentID string) (Job, error) {
	for _, job := range f.byID {
		if job.DocumentID == documentID {
			return job, nil
		}
	}
	return Job{}, errors.New("not found")
}

func (f *fakeJobStore) UpdateStatus(_ context.Context, id string, status JobStatus, message string) (Job, error) {
	job := f.byID[id]
	job.Status = status
	job.Error = message
	f.byID[id] = job
	return job, nil
}

func (f *fakeJobStore) UpdateProgress(_ context.Context, id string, status JobStatus, sparseChunks, embeddedChunks int) (Job, error) {
	job := f.byID[id]
	if f.progressErr != nil {
		return job, f.progressErr
	}
	job.Status = status
	job.SparseChunks = sparseChunks
	job.EmbeddedChunks = embeddedChunks
	f.byID[id] = job
	return job, nil
}

func (f *fakeJobStore) ListRecent(context.Context, int) ([]Job, error) {
	out := make([]Job, 0, len(f.byID))
	for _, job := range f.byID {
		out = append(out, job)
	}
	return out, nil
}
