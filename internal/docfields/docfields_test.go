package docfields

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Every refusal below happens before the store resolves a database: a Store with no resolver
// proves it, since reaching one would panic.
func TestRefusalsNeedNoDatabase(t *testing.T) {
	s, ctx := New(nil), context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"record without a key", func() error { _, err := s.Record(ctx, "id", "  "); return err }},
		{"unknown actor", func() error {
			return s.ApplyDiff(ctx, "id", []string{"r"}, Diff{}, "someone")
		}},
		{"rule actor without its label", func() error {
			return s.ApplyDiff(ctx, "id", []string{"r"}, Diff{}, "rule:")
		}},
		{"unknown field", func() error {
			return s.ApplyDiff(ctx, "id", []string{"r"}, Diff{Set: []Value{{Field: "colour", ValueKey: "red"}}}, SetByOperator)
		}},
		{"custom field without its definition", func() error {
			return s.ApplyDiff(ctx, "id", []string{"r"}, Diff{Set: []Value{{Field: "custom:", ValueKey: "x"}}}, SetByOperator)
		}},
		{"date not ISO", func() error {
			return s.ApplyDiff(ctx, "id", []string{"r"}, Diff{Set: []Value{{Field: FieldDocumentDate, ValueKey: "18/10/2026"}}}, SetByOperator)
		}},
		{"removal without a key", func() error {
			return s.ApplyDiff(ctx, "id", []string{"r"}, Diff{Remove: []ValueRef{{Field: FieldTag, ValueKey: " "}}}, SetByOperator)
		}},
		{"no document named", func() error { return s.ApplyDiff(ctx, "id", nil, Diff{}, SetByOperator) }},
		{"suggestion by the operator", func() error {
			_, err := s.Suggest(ctx, "id", "r", []Value{{Field: FieldTag, ValueKey: "lbl", SetBy: SetByOperator}})
			return err
		}},
		{"suggestion with a bad field", func() error {
			_, err := s.Suggest(ctx, "id", "r", []Value{{Field: "colour", ValueKey: "red", SetBy: SetByExtractor}})
			return err
		}},
		{"unknown kind listed", func() error { _, err := s.Labels(ctx, "id", "colour"); return err }},
		{"unknown kind saved", func() error { _, err := s.SaveLabel(ctx, "id", Label{Kind: "colour", Name: "x"}); return err }},
		{"empty name", func() error { _, err := s.SaveLabel(ctx, "id", Label{Kind: KindTag, Name: "  "}); return err }},
		{"name too long", func() error {
			_, err := s.SaveLabel(ctx, "id", Label{Kind: KindTag, Name: strings.Repeat("é", MaxNameRunes+1)})
			return err
		}},
		{"regex that does not parse", func() error {
			_, err := s.SaveLabel(ctx, "id", Label{Kind: KindTag, Name: "x", Algorithm: "regex", Pattern: "("})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

// A search that found nothing names nothing, and never resolves a database to do it: a Store with
// no resolver would panic.
func TestDocumentFieldsOfNoDocumentNeedNoDatabase(t *testing.T) {
	named, err := New(nil).DocumentFields(context.Background(), "id", nil)
	if err != nil || named == nil || len(named) != 0 {
		t.Fatalf("named = %v, %v", named, err)
	}
}

func TestNameKeyTrimsAndLowers(t *testing.T) {
	if got := NameKey("  Gialli IMPORT "); got != "gialli import" {
		t.Fatalf("NameKey = %q", got)
	}
}

func TestAffectedReadsTheCount(t *testing.T) {
	for _, tc := range []struct {
		rows []map[string]any
		want int
	}{
		{nil, 0},
		{[]map[string]any{{"count": float64(3)}}, 3},
		{[]map[string]any{{"other": "x"}}, 0},
	} {
		if got := affected(tc.rows); got != tc.want {
			t.Fatalf("affected(%v) = %d, want %d", tc.rows, got, tc.want)
		}
	}
}
