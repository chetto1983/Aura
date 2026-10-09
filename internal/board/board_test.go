package board

import (
	"errors"
	"strings"
	"testing"
	"testing/quick"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestCleanFieldsAppliesTheTablesRules(t *testing.T) {
	got, err := cleanFields("  call the supplier ", " ", 0, []string{" ops", "ops", "", "billing"})
	if err != nil {
		t.Fatalf("cleanFields: %v", err)
	}
	if got.label != "call the supplier" || got.description != "" || got.priority != 2 {
		t.Errorf("cleaned = %+v", got)
	}
	if strings.Join(got.tags, ",") != "ops,billing" {
		t.Errorf("tags = %v, want trimmed and deduplicated", got.tags)
	}
	for name, call := range map[string]func() error{
		"empty label":      func() error { _, e := cleanFields(" ", "", 2, nil); return e },
		"long label":       func() error { _, e := cleanFields(strings.Repeat("x", MaxLabelRunes+1), "", 2, nil); return e },
		"long description": func() error { _, e := cleanFields("x", strings.Repeat("y", MaxDescriptionRunes+1), 2, nil); return e },
		"priority 4":       func() error { _, e := cleanFields("x", "", 4, nil); return e },
		"long tag":         func() error { _, e := cleanFields("x", "", 2, []string{strings.Repeat("t", MaxTagRunes+1)}); return e },
		"too many tags": func() error {
			tags := make([]string, MaxTags+1)
			for i := range tags {
				tags[i] = strings.Repeat("t", i+1)
			}
			_, e := cleanFields("x", "", 2, tags)
			return e
		},
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := cleanFields(strings.Repeat("é", MaxLabelRunes), "", 3, nil); err != nil {
		t.Errorf("a label at the cap in runes, not bytes, was refused: %v", err)
	}
}

func TestValidateColumns(t *testing.T) {
	got, err := validateColumns([]Column{{ID: " todo ", Label: " To do "}, {ID: "done", Label: "Done", CardLimit: 3}})
	if err != nil || got[0].ID != "todo" || got[0].Label != "To do" {
		t.Fatalf("validateColumns = %+v, %v", got, err)
	}
	tooMany := make([]Column, MaxColumns+1)
	for i := range tooMany {
		tooMany[i] = Column{ID: strings.Repeat("c", i+1), Label: "c"}
	}
	for name, cols := range map[string][]Column{
		"none":           nil,
		"too many":       tooMany,
		"empty id":       {{ID: "", Label: "x"}},
		"empty label":    {{ID: "x", Label: " "}},
		"duplicate id":   {{ID: "x", Label: "a"}, {ID: "x", Label: "b"}},
		"negative limit": {{ID: "x", Label: "a", CardLimit: -1}},
	} {
		if _, err := validateColumns(cols); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
}

func card(id byte, position float64) sqlc.AuraBoardCards {
	var u pgtype.UUID
	u.Bytes[15], u.Valid = id, true
	return sqlc.AuraBoardCards{ID: u, Position: position}
}

func TestPositionBefore(t *testing.T) {
	siblings := []sqlc.AuraBoardCards{card(1, 1), card(2, 2), card(3, 4)}
	for name, tc := range map[string]struct {
		before string
		want   float64
	}{
		"bottom":         {"", 5},
		"unknown is end": {"ffffffff-ffff-ffff-ffff-ffffffffffff", 5},
		"top":            {siblings[0].ID.String(), 0},
		"middle":         {siblings[2].ID.String(), 3},
	} {
		got, ok := positionBefore(siblings, tc.before)
		if !ok || got != tc.want {
			t.Errorf("%s = %v, %v; want %v", name, got, ok, tc.want)
		}
	}
	if got, ok := positionBefore(nil, ""); !ok || got != 1 {
		t.Errorf("empty column = %v, %v; want 1", got, ok)
	}
	crowded := []sqlc.AuraBoardCards{card(1, 1), card(2, 1+positionGap/2)}
	if _, ok := positionBefore(crowded, crowded[1].ID.String()); ok {
		t.Error("neighbours closer than the gap must ask for a renumber")
	}
}

// Property: inserting before any sibling of a strictly increasing column keeps it strictly
// increasing, whenever positionBefore says the gap allows it.
func TestPositionBeforeKeepsTheOrder(t *testing.T) {
	f := func(gaps []uint8, pick uint8) bool {
		siblings := make([]sqlc.AuraBoardCards, 0, len(gaps))
		pos := 0.0
		for i, g := range gaps {
			pos += float64(g%7) + 0.5
			siblings = append(siblings, card(byte(i+1), pos))
		}
		before := ""
		at := len(siblings)
		if len(siblings) > 0 {
			at = int(pick) % (len(siblings) + 1)
			if at < len(siblings) {
				before = siblings[at].ID.String()
			}
		}
		got, ok := positionBefore(siblings, before)
		if !ok {
			return true
		}
		if at > 0 && !(got > siblings[at-1].Position) {
			return false
		}
		return at == len(siblings) || got < siblings[at].Position
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRefusesMalformedIdsBeforeTheDatabase(t *testing.T) {
	s := &Store{}
	ctx := t.Context()
	if _, err := s.AddCard(ctx, "not-a-uuid", NewCard{Label: "x"}, SourceCockpit, ActorOperator); !errors.Is(err, ErrInvalid) {
		t.Errorf("AddCard bad identity = %v", err)
	}
	if _, err := s.AddCard(ctx, "00000000-0000-0000-0000-000000000001", NewCard{Label: "x", TaskID: "nope"}, SourceCockpit, ActorOperator); !errors.Is(err, ErrInvalid) {
		t.Errorf("AddCard bad task id = %v", err)
	}
	if _, err := s.AddCard(ctx, "00000000-0000-0000-0000-000000000001", NewCard{Label: "x", ConversationID: "nope"}, SourceCockpit, ActorOperator); !errors.Is(err, ErrInvalid) {
		t.Errorf("AddCard bad conversation id = %v", err)
	}
	if _, err := s.DeleteCard(ctx, "x", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteCard bad id = %v", err)
	}
	if _, err := s.DeleteView(ctx, "x", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteView bad id = %v", err)
	}
	if _, err := s.Views(ctx, "nope"); !errors.Is(err, ErrInvalid) {
		t.Errorf("Views bad identity = %v", err)
	}
	if _, err := s.SaveView(ctx, "nope", "x", nil, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("SaveView bad identity = %v", err)
	}
	if _, err := s.SaveView(ctx, "00000000-0000-0000-0000-000000000001", " ", nil, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("SaveView empty name = %v", err)
	}
	if _, err := s.SaveView(ctx, "00000000-0000-0000-0000-000000000001", "x", []byte(`[1]`), false); !errors.Is(err, ErrInvalid) {
		t.Errorf("SaveView non-object filters = %v", err)
	}
	if _, err := s.Search(ctx, "00000000-0000-0000-0000-000000000001", "  ", 5); !errors.Is(err, ErrInvalid) {
		t.Errorf("Search empty = %v", err)
	}
	if _, err := s.SetColumns(ctx, "x", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("SetColumns empty = %v", err)
	}
}
