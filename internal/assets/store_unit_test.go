package assets

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	unitAssetID    = "11111111-1111-1111-1111-111111111111"
	unitIdentityID = "22222222-2222-2222-2222-222222222222"
)

var errUnitAssetScan = errors.New("unit asset scan sentinel")

type captureAssetDBTX struct {
	query  string
	args   []any
	called bool
}

func (db *captureAssetDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errUnitAssetScan
}

func (db *captureAssetDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errUnitAssetScan
}

func (db *captureAssetDBTX) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.query = query
	db.args = append([]any(nil), args...)
	db.called = true
	return assetScanErrorRow{}
}

type assetScanErrorRow struct{}

func (assetScanErrorRow) Scan(...any) error {
	return errUnitAssetScan
}

func TestStoreLifecycleUpdatesScopeMutationsToIdentity(t *testing.T) {
	for _, method := range []string{"MarkUploaded", "MarkAccepted", "SetStatus", "SetResult"} {
		t.Run(method, func(t *testing.T) {
			db := &captureAssetDBTX{}
			store := NewStore(db)

			err := invokeLifecycleMethod(t, store, method, unitAssetID, unitIdentityID)
			if !errors.Is(err, errUnitAssetScan) {
				t.Fatalf("%s error = %v, want sentinel scan error", method, err)
			}
			if !db.called {
				t.Fatalf("%s did not execute a query", method)
			}

			query := compactSQL(db.query)
			if !strings.Contains(query, "and identity_id = $") {
				t.Errorf("%s SQL missing identity guard: %s", method, query)
			}
			if !strings.Contains(query, "and deleted_at is null") {
				t.Errorf("%s SQL missing deleted guard: %s", method, query)
			}
			if !uuidArgEquals(db.args, 1, unitIdentityID) {
				t.Errorf("%s arg $2 = %#v, want identity UUID %s; args: %#v", method, argAt(db.args, 1), unitIdentityID, db.args)
			}
		})
	}
}

func TestStoreLifecycleUpdatesValidateUUIDs(t *testing.T) {
	for _, method := range []string{"MarkUploaded", "MarkAccepted", "SetStatus", "SetResult"} {
		t.Run(method+" invalid asset id", func(t *testing.T) {
			db := &captureAssetDBTX{}
			store := NewStore(db)

			err := invokeLifecycleMethod(t, store, method, "not-a-uuid", unitIdentityID)
			if err == nil || !strings.Contains(err.Error(), `invalid asset id "not-a-uuid"`) {
				t.Fatalf("%s invalid asset error = %v", method, err)
			}
			if db.called {
				t.Fatalf("%s queried database after invalid asset id", method)
			}
		})

		t.Run(method+" invalid identity id", func(t *testing.T) {
			db := &captureAssetDBTX{}
			store := NewStore(db)

			err := invokeLifecycleMethod(t, store, method, unitAssetID, "not-a-uuid")
			if err == nil || !strings.Contains(err.Error(), `invalid identity_id "not-a-uuid"`) {
				t.Fatalf("%s invalid identity error = %v", method, err)
			}
			if db.called {
				t.Fatalf("%s queried database after invalid identity id", method)
			}
		})
	}
}

func invokeLifecycleMethod(t *testing.T, store *Store, method, assetID, identityID string) error {
	t.Helper()

	target := reflect.ValueOf(store).MethodByName(method)
	if !target.IsValid() {
		t.Fatalf("missing Store.%s", method)
	}

	var args []reflect.Value
	ctx := context.Background()
	switch method {
	case "MarkUploaded":
		args = lifecycleArgs(t, target, []any{ctx, assetID, int64(42), "etag-unit"}, []any{ctx, assetID, identityID, int64(42), "etag-unit"})
	case "MarkAccepted":
		args = lifecycleArgs(t, target, []any{ctx, assetID, int64(42), "hash-unit", "application/pdf"}, []any{ctx, assetID, identityID, int64(42), "hash-unit", "application/pdf"})
	case "SetStatus":
		args = lifecycleArgs(t, target, []any{ctx, assetID, StatusFailed, "code-unit", "message-unit"}, []any{ctx, assetID, identityID, StatusFailed, "code-unit", "message-unit"})
	case "SetResult":
		result := Result{Status: StatusSearchable, DocumentID: "doc-unit", Summary: "summary-unit", Metadata: map[string]any{"source": "unit"}}
		args = lifecycleArgs(t, target, []any{ctx, assetID, result}, []any{ctx, assetID, identityID, result})
	default:
		t.Fatalf("unknown lifecycle method %s", method)
	}

	out := target.Call(args)
	if len(out) != 2 {
		t.Fatalf("%s returned %d values, want 2", method, len(out))
	}
	if out[1].IsNil() {
		return nil
	}
	return out[1].Interface().(error)
}

func lifecycleArgs(t *testing.T, target reflect.Value, legacy, scoped []any) []reflect.Value {
	t.Helper()

	switch target.Type().NumIn() {
	case len(scoped):
		return reflectValues(scoped)
	case len(legacy):
		return reflectValues(legacy)
	default:
		t.Fatalf("%s has %d args, want %d with identity_id", target.Type(), target.Type().NumIn(), len(scoped))
		return nil
	}
}

func reflectValues(args []any) []reflect.Value {
	values := make([]reflect.Value, 0, len(args))
	for _, arg := range args {
		values = append(values, reflect.ValueOf(arg))
	}
	return values
}

func compactSQL(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func argAt(args []any, index int) any {
	if index < 0 || index >= len(args) {
		return nil
	}
	return args[index]
}

func uuidArgEquals(args []any, index int, want string) bool {
	if index < 0 || index >= len(args) {
		return false
	}
	wantUUID := uuid.MustParse(want)
	value, ok := args[index].(pgtype.UUID)
	if !ok || !value.Valid {
		return false
	}
	return uuid.UUID(value.Bytes) == wantUUID
}
