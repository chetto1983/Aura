package arcadedb

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const memoryBatchReceiptType = "MemoryBatchReceipt"

func memoryBatchSchemaStatements() []string {
	return []string{
		"CREATE VERTEX TYPE " + memoryBatchReceiptType + " IF NOT EXISTS",
		"CREATE PROPERTY " + memoryBatchReceiptType + ".receipt_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + memoryBatchReceiptType + ".identity_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + memoryBatchReceiptType + ".idempotency_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + memoryBatchReceiptType + ".request_hash IF NOT EXISTS STRING",
		"CREATE PROPERTY " + memoryBatchReceiptType + ".result_json IF NOT EXISTS STRING",
		"CREATE PROPERTY " + memoryBatchReceiptType + ".committed_at IF NOT EXISTS DATETIME",
		"CREATE INDEX IF NOT EXISTS ON " + memoryBatchReceiptType + " (receipt_key) UNIQUE",
	}
}

type clientMemoryBatchTx struct {
	client    *Client
	sessionID string
	identity  string
	closed    bool
}

func (backend clientMemoryBatchBackend) EmbedStatements(
	ctx context.Context,
	statements []string,
) map[string][]float64 {
	return backend.client.embedStatements(ctx, statements)
}

func (backend clientMemoryBatchBackend) Begin(
	ctx context.Context,
	identity string,
) (memoryBatchTransaction, error) {
	if backend.client == nil {
		return nil, fmt.Errorf("arcadedb: nil memory batch client")
	}
	sessionID, err := backend.client.beginTx(ctx)
	if err != nil {
		return nil, err
	}
	return &clientMemoryBatchTx{
		client: backend.client, sessionID: sessionID, identity: identity,
	}, nil
}

func (tx *clientMemoryBatchTx) LoadReceipt(
	ctx context.Context,
	receiptKey string,
) (*memoryBatchReceipt, error) {
	rows, err := tx.client.queryInTx(ctx, tx.sessionID,
		"SELECT identity_id, idempotency_key, request_hash, result_json FROM "+
			memoryBatchReceiptType+" WHERE receipt_key = :receipt_key LIMIT 1",
		map[string]any{"receipt_key": receiptKey})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	receipt := &memoryBatchReceipt{
		IdentityID:     rowString(rows[0], "identity_id"),
		IdempotencyKey: rowString(rows[0], "idempotency_key"),
		RequestHash:    rowString(rows[0], "request_hash"),
	}
	if receipt.IdentityID != tx.identity {
		return nil, fmt.Errorf("receipt identity does not match authenticated identity")
	}
	if err := json.Unmarshal([]byte(rowString(rows[0], "result_json")), &receipt.Result); err != nil {
		return nil, fmt.Errorf("decode committed result: %w", err)
	}
	return receipt, nil
}

func (tx *clientMemoryBatchTx) LoadState(ctx context.Context) (memoryBatchState, error) {
	state := memoryBatchState{Entities: map[string]memoryBatchEntity{}, Facts: map[string]memoryBatchFact{}}
	entityRows, err := tx.client.queryInTx(ctx, tx.sessionID,
		"SELECT name, kind FROM Entity", nil)
	if err != nil {
		return memoryBatchState{}, fmt.Errorf("read entities: %w", err)
	}
	for _, row := range entityRows {
		name := rowString(row, "name")
		if name != "" {
			// No class is read back here: an entity that already exists keeps the class
			// it was minted with, and entityClassScan below reports that authoritatively.
			state.Entities[name] = memoryBatchEntity{Kind: rowString(row, "kind")}
		}
	}
	factRows, err := tx.client.queryInTx(ctx, tx.sessionID,
		"SELECT @rid AS rid, statement, predicate, valid_from, valid_to, created_at, "+
			"expired_at, fact_key, sources, embedding, outV().name AS subject, "+
			"outV().kind AS subject_kind, inV().name AS object, inV().kind AS object_kind "+
			"FROM "+factEdgeType, nil)
	if err != nil {
		return memoryBatchState{}, fmt.Errorf("read facts: %w", err)
	}
	for _, row := range factRows {
		rid := rowString(row, "rid")
		if rid == "" {
			return memoryBatchState{}, fmt.Errorf("fact row has no RID")
		}
		validFrom, err := parseMemoryBatchTime(rowString(row, "valid_from"))
		if err != nil {
			return memoryBatchState{}, fmt.Errorf("fact %s valid_from: %w", rid, err)
		}
		validTo, err := parseMemoryBatchTime(rowString(row, "valid_to"))
		if err != nil {
			return memoryBatchState{}, fmt.Errorf("fact %s valid_to: %w", rid, err)
		}
		createdAt, err := parseMemoryBatchTime(rowString(row, "created_at"))
		if err != nil {
			return memoryBatchState{}, fmt.Errorf("fact %s created_at: %w", rid, err)
		}
		expiredAt, err := parseMemoryBatchTime(rowString(row, "expired_at"))
		if err != nil {
			return memoryBatchState{}, fmt.Errorf("fact %s expired_at: %w", rid, err)
		}
		sources := factSources(row["sources"])
		fact := Fact{
			Subject: rowString(row, "subject"), SubjectKind: rowString(row, "subject_kind"),
			Predicate: rowString(row, "predicate"), Object: rowString(row, "object"),
			ObjectKind: rowString(row, "object_kind"), Statement: rowString(row, "statement"),
			ValidFrom: validFrom, ValidTo: validTo,
		}
		if len(sources) > 0 {
			fact.Source = sources[0]
		}
		state.Facts[rid] = memoryBatchFact{
			RID: rid, Fact: fact, Sources: sources, ValidFrom: validFrom, ValidTo: validTo,
			CreatedAt: createdAt, ExpiredAt: expiredAt, FactKey: rowString(row, "fact_key"),
			Embedding: row["embedding"],
		}
	}
	return state, nil
}

func (tx *clientMemoryBatchTx) Persist(
	ctx context.Context,
	before memoryBatchState,
	after memoryBatchState,
) error {
	deletedRIDs := []string{}
	for key, oldFact := range before.Facts {
		newFact, exists := after.Facts[key]
		if !exists || memoryBatchEndpointsChanged(oldFact, newFact) {
			deletedRIDs = append(deletedRIDs, oldFact.RID)
		}
	}
	sort.Strings(deletedRIDs)
	if len(deletedRIDs) > 0 {
		if _, err := tx.client.commandInTx(ctx, tx.sessionID,
			"DELETE FROM "+factEdgeType+" WHERE @rid IN :rids",
			map[string]any{"rids": deletedRIDs}); err != nil {
			return fmt.Errorf("delete replaced facts: %w", err)
		}
	}

	// Same class rule as UpsertFact: an entity that already exists keeps its class, a new
	// one is minted in the class the operation asked for, or failing that the one its kind
	// implies, or failing that Other.
	toUpsert := memoryBatchEntitiesToUpsert(before, after)
	heldClasses, err := tx.client.entityClassScan(ctx, toUpsert...)
	if err != nil {
		return err
	}
	for _, name := range toUpsert {
		entity := after.Entities[name]
		kind := entity.Kind
		class, _ := poleClassFor(entity.Pole, kind)
		if existing := heldClasses[name]; existing != "" {
			class = existing
		}
		params := map[string]any{"name": name}
		if kind != "" {
			params["kind"] = kind
		}
		if _, err := tx.client.commandInTx(ctx, tx.sessionID,
			upsertEntityInClass(class, kind != ""), params); err != nil {
			return fmt.Errorf("upsert entity %q as %s: %w", name, class, err)
		}
	}

	for _, key := range sortedMemoryBatchFactKeys(after) {
		newFact := after.Facts[key]
		oldFact, existed := before.Facts[key]
		if existed && !memoryBatchEndpointsChanged(oldFact, newFact) {
			if memoryBatchStoredFactsEqual(oldFact, newFact) {
				continue
			}
			if err := tx.updateFact(ctx, newFact); err != nil {
				return err
			}
			continue
		}
		if err := tx.createFact(ctx, newFact); err != nil {
			return err
		}
	}

	for _, name := range sortedMemoryBatchEntities(before.Entities) {
		if _, keep := after.Entities[name]; keep {
			continue
		}
		if _, err := tx.client.commandInTx(ctx, tx.sessionID,
			"DELETE FROM Entity WHERE name = :name AND bothE().size() = 0",
			map[string]any{"name": name}); err != nil {
			return fmt.Errorf("prune entity %q: %w", name, err)
		}
	}
	return nil
}

func memoryBatchEntitiesToUpsert(before, after memoryBatchState) []string {
	needed := map[string]memoryBatchEntity{}
	for name, entity := range after.Entities {
		if old, exists := before.Entities[name]; !exists || old != entity {
			needed[name] = entity
		}
	}
	for key, fact := range after.Facts {
		oldFact, exists := before.Facts[key]
		if exists && !memoryBatchEndpointsChanged(oldFact, fact) {
			continue
		}
		needed[fact.Fact.Subject] = after.Entities[fact.Fact.Subject]
		needed[fact.Fact.Object] = after.Entities[fact.Fact.Object]
	}
	return sortedMemoryBatchEntities(needed)
}

func (tx *clientMemoryBatchTx) updateFact(ctx context.Context, fact memoryBatchFact) error {
	_, err := tx.client.commandInTx(ctx, tx.sessionID,
		"UPDATE "+factEdgeType+" SET statement = :statement, predicate = :predicate, "+
			"valid_from = :valid_from, valid_to = :valid_to, created_at = :created_at, "+
			"expired_at = :expired_at, fact_key = :fact_key, sources = :sources WHERE @rid = :rid",
		memoryBatchFactParams(fact))
	if err != nil {
		return fmt.Errorf("update fact %s: %w", fact.RID, err)
	}
	return nil
}

func (tx *clientMemoryBatchTx) createFact(ctx context.Context, fact memoryBatchFact) error {
	params := memoryBatchFactParams(fact)
	params["subject_name"] = fact.Fact.Subject
	params["object_name"] = fact.Fact.Object
	statement := createFactStatement
	if fact.Embedding != nil {
		params["embedding"] = fact.Embedding
		statement += createFactEmbeddingClause
	}
	if _, err := tx.client.commandInTx(ctx, tx.sessionID, statement, params); err != nil {
		return fmt.Errorf("create fact %q: %w", fact.Fact.Statement, err)
	}
	return nil
}

func (tx *clientMemoryBatchTx) SaveReceipt(
	ctx context.Context,
	receiptKey string,
	receipt memoryBatchReceipt,
) error {
	encoded, err := json.Marshal(receipt.Result)
	if err != nil {
		return fmt.Errorf("encode committed result: %w", err)
	}
	_, err = tx.client.commandInTx(ctx, tx.sessionID,
		"UPDATE "+memoryBatchReceiptType+" SET identity_id = :identity_id, "+
			"idempotency_key = :idempotency_key, request_hash = :request_hash, "+
			"result_json = :result_json, committed_at = :committed_at UPSERT "+
			"WHERE receipt_key = :receipt_key",
		map[string]any{
			"receipt_key": receiptKey, "identity_id": receipt.IdentityID,
			"idempotency_key": receipt.IdempotencyKey, "request_hash": receipt.RequestHash,
			"result_json": string(encoded), "committed_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	if err != nil {
		return err
	}
	return nil
}

func (tx *clientMemoryBatchTx) Commit(ctx context.Context) error {
	if tx.closed {
		return fmt.Errorf("memory batch transaction already closed")
	}
	if err := tx.client.commitTx(ctx, tx.sessionID); err != nil {
		return err
	}
	tx.closed = true
	return nil
}

func (tx *clientMemoryBatchTx) Rollback(ctx context.Context) {
	if tx.closed {
		return
	}
	tx.client.rollbackTx(ctx, tx.sessionID)
	tx.closed = true
}

func memoryBatchFactParams(fact memoryBatchFact) map[string]any {
	return map[string]any{
		"rid": fact.RID, "statement": fact.Fact.Statement, "predicate": fact.Fact.Predicate,
		"valid_from": nullableMemoryBatchTime(fact.ValidFrom),
		"valid_to":   nullableMemoryBatchTime(fact.ValidTo),
		"created_at": nullableMemoryBatchTime(fact.CreatedAt),
		"expired_at": nullableMemoryBatchTime(fact.ExpiredAt),
		"fact_key":   nullableMemoryBatchString(fact.FactKey),
		"sources":    sourcesParam(fact.Sources),
	}
}

func memoryBatchEndpointsChanged(a, b memoryBatchFact) bool {
	return a.Fact.Subject != b.Fact.Subject || a.Fact.Object != b.Fact.Object
}

func memoryBatchStoredFactsEqual(a, b memoryBatchFact) bool {
	a.RID, b.RID = "", ""
	a.Fact.Source, b.Fact.Source = FactSource{}, FactSource{}
	return reflect.DeepEqual(a, b)
}

func sortedMemoryBatchEntities(entities map[string]memoryBatchEntity) []string {
	names := make([]string, 0, len(entities))
	for name := range entities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func parseMemoryBatchTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	// ArcadeDB renders DATETIME with its documented default without a zone,
	// even when Aura inserted an RFC3339 UTC value. Aura's memory timestamps
	// are UTC, so restore the zone the wire representation omits.
	// https://docs.arcadedb.com/arcadedb/reference/managing-dates
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func nullableMemoryBatchTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	// FACT.valid_from/valid_to are ArcadeDB DATETIME properties, whose documented
	// precision is milliseconds while the database's default formatter is seconds.
	// The established UpsertFact/read path uses second-precision RFC3339 on both
	// sides. Keeping the batch path identical avoids a live as-of miss after the
	// engine truncates a nanosecond input to DATETIME precision. Capture provenance
	// retains the exact observed_at separately.
	// https://docs.arcadedb.com/arcadedb/reference/managing-dates
	return value.UTC().Format(time.RFC3339)
}

func nullableMemoryBatchString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
