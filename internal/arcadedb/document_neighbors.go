package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PassageRef names one passage by the document it belongs to and its position in it.
type PassageRef struct {
	SearchDocumentID string
	Ordinal          int64
}

// passageKey is the key services/ingest writes, and the UNIQUE index this reads through.
// The format is that module's contract -- app.py builds it as f"{search_document_id}:{ordinal}"
// -- so it is stated here once rather than inferred at each call site.
func (r PassageRef) passageKey() string {
	return r.SearchDocumentID + ":" + strconv.FormatInt(r.Ordinal, 10)
}

// PassagesAt returns the named passages, unranked, for the ones that exist.
//
// A passage that is not there is simply absent from the result: the caller asks for the
// neighbours of a hit, and a hit at the first or last ordinal of its document has none on
// one side. That is also what makes the key format safe to depend on -- were services/ingest
// to write a different one, the lookup would return nothing and the answer would lose its
// surrounding context, never gain the wrong one.
func (d *DocumentIndex) PassagesAt(
	ctx context.Context,
	identityID string,
	refs []PassageRef,
) ([]PassageCandidate, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if len(refs) > d.config.MaxRetrievalCandidates {
		return nil, fmt.Errorf(
			"arcadedb: %d passage references exceed maximum %d",
			len(refs), d.config.MaxRetrievalCandidates,
		)
	}
	// Every reference is checked BEFORE any client is acquired: resolving a tenant is
	// itself I/O, so validating afterwards would have already touched the server on behalf
	// of a request that was never going to be sent.
	keys := make([]string, 0, len(refs))
	wanted := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref.SearchDocumentID = strings.TrimSpace(ref.SearchDocumentID)
		if err := validateIdentifier("passage document id", ref.SearchDocumentID); err != nil {
			return nil, err
		}
		if ref.Ordinal < 0 {
			return nil, fmt.Errorf("arcadedb: passage ordinal must not be negative")
		}
		key := ref.passageKey()
		if _, duplicate := wanted[key]; duplicate {
			continue
		}
		wanted[key] = struct{}{}
		keys = append(keys, key)
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := client.Query(ctx,
		"SELECT "+passageCandidateFields+" FROM "+documentPassageType+
			" WHERE passage_key IN :passage_keys",
		map[string]any{"passage_keys": keys},
	)
	if err != nil {
		if missingIngestType(err, documentPassageType) {
			return nil, nil // nothing ingested yet — an empty library, not a failure
		}
		return nil, fmt.Errorf("arcadedb: passages at ordinal: %w", err)
	}
	if len(rows) > len(keys) {
		return nil, fmt.Errorf(
			"arcadedb: passage lookup returned %d rows for %d keys", len(rows), len(keys),
		)
	}
	passages := make([]PassageCandidate, 0, len(rows))
	for index, row := range rows {
		passage, key, err := d.decodePassageRow(row)
		if err != nil {
			return nil, fmt.Errorf("arcadedb: passage %d: %w", index, err)
		}
		// The key was asked for by name, so a row carrying any other one means the engine
		// answered a question that was not put to it.
		if _, asked := wanted[key]; !asked {
			return nil, fmt.Errorf("arcadedb: passage lookup returned unrequested %s", key)
		}
		passages = append(passages, passage)
	}
	return passages, nil
}
