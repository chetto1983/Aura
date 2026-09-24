package arcadedb

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// The per-tenant picture behind the cockpit's embedding card and `aura doctor` (spec §4,
// §10): where every stamped row stands against the space a family is read in, which documents
// hold the documents family shut, and what re-embedding into a new space would cost. It reads
// the same stamps and filters the gate and the pass read, so the card cannot disagree with
// them.

// TypeTally is one type's rows against a family's space.
type TypeTally struct {
	Type       string `json:"type"`
	InSpace    int    `json:"in_space"`
	OtherSpace int    `json:"other_space"`
	NoVector   int    `json:"no_vector"`
	// Rejected rows have no vector and carry this space's stamp: the model refused their text,
	// and the pass will not ask it again (spec §5).
	Rejected int `json:"rejected"`
}

// FamilyState is one family's gate for one tenant: Open exactly when no type has a vector in
// another space, the gate's own rule (spec §3).
type FamilyState struct {
	Family string      `json:"family"`
	Space  string      `json:"space"`
	Open   bool        `json:"open"`
	Types  []TypeTally `json:"types"`
}

// StuckDocument is a document whose rows are still in another space. CocoIndex keeps a failing
// document's old rows, so this is how a file that will not re-index is named (spec §6).
type StuckDocument struct {
	FileName  string `json:"file_name"`
	SourceKey string `json:"source_key"`
	Space     string `json:"space,omitempty"` // empty when the rows were never stamped
}

// TenantSpaceReport is one identity's embedding state.
type TenantSpaceReport struct {
	IdentityID     string          `json:"identity_id"`
	Families       []FamilyState   `json:"families"`
	StuckDocuments []StuckDocument `json:"stuck_documents"`
	// IngestStatus and IngestErrors are the tenant's IngestStatus row: CocoIndex reports
	// failures as counts, never per file.
	IngestStatus string `json:"ingest_status,omitempty"`
	IngestErrors int    `json:"ingest_errors"`
}

// TypeWork is what a new space would re-embed of one type.
type TypeWork struct {
	Type  string `json:"type"`
	Rows  int    `json:"rows"`
	Chars int    `json:"chars"`
}

// CorpusWork is what a new space would re-embed across the corpus.
type CorpusWork struct {
	Types []TypeWork `json:"types"`
	// PassagesOverLimit counts passages longer than the limit in characters, a lower bound on
	// the UTF-8 bytes the hosted route's cut measures (spec §6).
	PassagesOverLimit int `json:"passages_over_limit"`
}

// stuckDocumentsShown bounds the list: it names files for an operator, not a corpus export.
const stuckDocumentsShown = 50

// MemoryDimensions is the memory family's pinned width (spec §1): a route whose model answers
// narrower cannot fill a memory vector.
const MemoryDimensions = vectorDimensions

var (
	// documentSpaceTypes are the documents family (spec §3) with the text each vector embeds.
	documentSpaceTypes = []memorySpaceType{
		{name: documentPassageType, source: "`text`"},
		{name: IndexedDocumentType, source: "card"},
	}

	stuckDocumentsStatement = "SELECT file_name, source_key, embed_space FROM " + IndexedDocumentType +
		" WHERE embedding IS NOT NULL AND " + otherSpace + " ORDER BY file_name LIMIT " + strconv.Itoa(stuckDocumentsShown)

	passagesOverLimitStatement = "SELECT count(*) AS n FROM " + documentPassageType + " WHERE `text`.length() > :limit"
)

func spaceParams(space string) map[string]any {
	return map[string]any{"space": space, "now": time.Now().UTC().Format(time.RFC3339Nano)}
}

// countRows runs one count. A type the tenant does not have yet counts zero: ingest declares
// its types on its first run, and a memory never written to has no rows either.
func (c *Client) countRows(ctx context.Context, statement string, params map[string]any, typeName string) (int, int, error) {
	rows, err := c.Query(ctx, statement, params)
	if err != nil {
		if missingIngestType(err, typeName) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("arcadedb: count %s: %w", typeName, err)
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	return int(rowInt(rows[0], "n")), int(rowInt(rows[0], "chars")), nil
}

func (c *Client) familyState(ctx context.Context, family string, types []memorySpaceType, space string) (FamilyState, error) {
	state := FamilyState{Family: family, Space: space, Open: true, Types: make([]TypeTally, 0, len(types))}
	params := spaceParams(space)
	for _, t := range types {
		tally := TypeTally{Type: t.name}
		from := "SELECT count(*) AS n FROM " + t.name + " WHERE "
		for _, count := range []struct {
			target    *int
			statement string
		}{
			{&tally.InSpace, from + "embedding IS NOT NULL AND embed_space = :space" + t.live},
			{&tally.OtherSpace, vectorsOutside(t.name, t.live)},
			{&tally.NoVector, from + "embedding IS NULL" + t.live},
			{&tally.Rejected, from + "embedding IS NULL AND embed_space = :space" + t.live},
		} {
			n, _, err := c.countRows(ctx, count.statement, params, t.name)
			if err != nil {
				return FamilyState{}, err
			}
			*count.target = n
		}
		state.Open = state.Open && tally.OtherSpace == 0
		state.Types = append(state.Types, tally)
	}
	return state, nil
}

// SpaceReport reads this tenant's rows against the memory family's space and the documents
// family's: the two can differ in width outside the pinned deployment (spec §1).
func (c *Client) SpaceReport(ctx context.Context, identityID, memorySpace, documentSpace string) (TenantSpaceReport, error) {
	report := TenantSpaceReport{IdentityID: identityID, StuckDocuments: []StuckDocument{}}
	for _, family := range []struct {
		name  string
		types []memorySpaceType
		space string
	}{{"memory", memorySpaceTypes, memorySpace}, {"documents", documentSpaceTypes, documentSpace}} {
		state, err := c.familyState(ctx, family.name, family.types, family.space)
		if err != nil {
			return TenantSpaceReport{}, err
		}
		report.Families = append(report.Families, state)
	}
	rows, err := c.Query(ctx, stuckDocumentsStatement, spaceParams(documentSpace))
	if err != nil && !missingIngestType(err, IndexedDocumentType) {
		return TenantSpaceReport{}, fmt.Errorf("arcadedb: stuck documents: %w", err)
	}
	for _, row := range rows {
		report.StuckDocuments = append(report.StuckDocuments, StuckDocument{
			FileName: rowString(row, "file_name"), SourceKey: rowString(row, "source_key"), Space: rowString(row, "embed_space"),
		})
	}
	state, err := c.ingestState(ctx, identityID)
	if err != nil {
		return TenantSpaceReport{}, err
	}
	if state != nil {
		report.IngestStatus, report.IngestErrors = state.Status, int(state.Errors)
	}
	return report, nil
}

// CorpusWork measures what moving each family to its target space would re-embed: every row
// with text whose stamp is not the target. limitChars > 0 also counts the passages a hosted
// model's input limit would cut.
func (c *Client) CorpusWork(ctx context.Context, memorySpace, documentSpace string, limitChars int) (CorpusWork, error) {
	work := CorpusWork{}
	for _, family := range []struct {
		types []memorySpaceType
		space string
	}{{memorySpaceTypes, memorySpace}, {documentSpaceTypes, documentSpace}} {
		for _, t := range family.types {
			statement := "SELECT count(*) AS n, sum(" + t.source + ".length()) AS chars FROM " + t.name +
				" WHERE " + t.source + " IS NOT NULL AND " + otherSpace + t.live
			rows, chars, err := c.countRows(ctx, statement, spaceParams(family.space), t.name)
			if err != nil {
				return CorpusWork{}, err
			}
			work.Types = append(work.Types, TypeWork{Type: t.name, Rows: rows, Chars: chars})
		}
	}
	if limitChars > 0 {
		over, _, err := c.countRows(ctx, passagesOverLimitStatement, map[string]any{"limit": limitChars}, documentPassageType)
		if err != nil {
			return CorpusWork{}, err
		}
		work.PassagesOverLimit = over
	}
	return work, nil
}

// SpaceReports reads every provisioned tenant's report.
func (b *TenantBackfill) SpaceReports(ctx context.Context, memorySpace, documentSpace string) ([]TenantSpaceReport, error) {
	reports := []TenantSpaceReport{}
	err := b.eachTenant(ctx, func(ctx context.Context, client *Client, identityID string) error {
		report, err := client.SpaceReport(ctx, identityID, memorySpace, documentSpace)
		if err == nil {
			reports = append(reports, report)
		}
		return err
	})
	return reports, err
}

// CorpusWork sums every provisioned tenant's work, type by type.
func (b *TenantBackfill) CorpusWork(ctx context.Context, memorySpace, documentSpace string, limitChars int) (CorpusWork, error) {
	var total CorpusWork
	err := b.eachTenant(ctx, func(ctx context.Context, client *Client, _ string) error {
		work, err := client.CorpusWork(ctx, memorySpace, documentSpace, limitChars)
		if err != nil {
			return err
		}
		if total.Types == nil {
			total.Types = make([]TypeWork, len(work.Types))
		}
		for index, typed := range work.Types {
			total.Types[index].Type = typed.Type
			total.Types[index].Rows += typed.Rows
			total.Types[index].Chars += typed.Chars
		}
		total.PassagesOverLimit += work.PassagesOverLimit
		return nil
	})
	return total, err
}

// eachTenant runs read against every provisioned tenant, in roster order. A tenant that fails
// is logged and left out; the walk fails only when no tenant answered and one failed, so one
// broken database does not blank the whole card.
func (b *TenantBackfill) eachTenant(ctx context.Context, read func(context.Context, *Client, string) error) error {
	if b == nil || b.identities == nil || b.credentials == nil {
		return fmt.Errorf("arcadedb: the embedding space report is not configured")
	}
	identities, err := b.identities.IdentityIDs(ctx)
	if err != nil {
		return fmt.Errorf("arcadedb: list identities for the embedding space report: %w", err)
	}
	answered := 0
	var firstErr error
	for _, identityID := range identities {
		_, provisioned, err := b.sweepTenant(ctx, identityID, func(ctx context.Context, client *Client, _ string) (int, error) {
			return 0, read(ctx, client, identityID)
		})
		switch {
		case err != nil:
			slog.Warn("embedding space report: tenant failed", "identity", identityID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		case provisioned:
			answered++
		}
	}
	if answered == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}
