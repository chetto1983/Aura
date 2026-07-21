package agui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

func TestObjectStoreExportDestinationIsDurableAndOwnerScoped(t *testing.T) {
	t.Parallel()

	objects := objectstore.NewFake()
	destination := NewObjectStoreExportDestination(objects, "exports")
	owner := uuid.NewString()
	exportID := uuid.NewString()
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := destination.Publish(context.Background(), owner, exportID, strings.NewReader("zip"), 3, "ignored", expiresAt); err != nil {
		t.Fatal(err)
	}
	body, err := destination.Open(context.Background(), owner, exportID, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	_ = body.Close()
	if string(got) != "zip" {
		t.Fatalf("archive = %q", got)
	}
	if _, err := destination.Open(context.Background(), uuid.NewString(), exportID, expiresAt); err == nil {
		t.Fatal("foreign owner opened durable export")
	}
}
