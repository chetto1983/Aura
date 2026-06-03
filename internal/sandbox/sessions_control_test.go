package sandbox

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// activeRow builds an in-memory active session row for the control-plane tests.
func activeRow(idByte byte, convByte byte, containerID string) sqlc.AuraSandboxSessions {
	return sqlc.AuraSandboxSessions{
		ID:             pgtype.UUID{Bytes: [16]byte{idByte}, Valid: true},
		ConversationID: pgtype.UUID{Bytes: [16]byte{convByte}, Valid: true},
		ContainerID:    containerID,
		Status:         "active",
	}
}

func TestSessionControl_List(t *testing.T) {
	st := &fakeStore{active: []sqlc.AuraSandboxSessions{
		activeRow(1, 0xa1, "cont-1"),
		activeRow(2, 0xa2, "cont-2"),
	}}
	ctrl := newSessionControl(&fakeDocker{}, st)

	rows, err := ctrl.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].ContainerID != "cont-1" || rows[0].Status != "active" {
		t.Fatalf("row projection mismatch: %+v", rows[0])
	}
	if rows[0].StartedAt != "-" {
		t.Fatalf("a NULL timestamp must render as '-', got %q", rows[0].StartedAt)
	}
}

func TestSessionControl_TerminateStopsAndMarks(t *testing.T) {
	row := activeRow(3, 0xb7, "cont-victim")
	st := &fakeStore{active: []sqlc.AuraSandboxSessions{row}}
	dk := &fakeDocker{}
	ctrl := newSessionControl(dk, st)

	convID := row.ConversationID.String()
	if err := ctrl.Terminate(context.Background(), convID); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if len(dk.stopped) != 1 || dk.stopped[0] != "cont-victim" {
		t.Fatalf("Terminate must docker-stop the container: %v", dk.stopped)
	}
	if len(dk.removed) != 1 || dk.removed[0] != "cont-victim" {
		t.Fatalf("Terminate must docker-rm the container: %v", dk.removed)
	}
	if st.terminatedCount() != 1 {
		t.Fatalf("Terminate must MarkTerminated the row, got %d", st.terminatedCount())
	}
}

func TestSessionControl_TerminateNotFound(t *testing.T) {
	st := &fakeStore{active: []sqlc.AuraSandboxSessions{activeRow(4, 0xc1, "cont-x")}}
	ctrl := newSessionControl(&fakeDocker{}, st)

	if err := ctrl.Terminate(context.Background(), pgtype.UUID{Bytes: [16]byte{0xff}, Valid: true}.String()); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("an unknown conversation must return ErrSessionNotFound, got %v", err)
	}
}

func TestSessionControl_PruneAll(t *testing.T) {
	st := &fakeStore{active: []sqlc.AuraSandboxSessions{
		activeRow(5, 0xd1, "cont-a"),
		activeRow(6, 0xd2, "cont-b"),
	}}
	dk := &fakeDocker{}
	ctrl := newSessionControl(dk, st)

	n, err := ctrl.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 2 {
		t.Fatalf("Prune must report 2 pruned rows, got %d", n)
	}
	if len(dk.removed) != 2 || st.terminatedCount() != 2 {
		t.Fatalf("Prune must stop/rm + MarkTerminated every active row: removed=%v terminated=%d", dk.removed, st.terminatedCount())
	}
}
