package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSummaryGenerator struct {
	out []byte
	err error
}

func (f *fakeSummaryGenerator) GenerateSummary(context.Context, []byte) ([]byte, error) {
	return f.out, f.err
}

func validSummaryJSON() []byte {
	return []byte(`{"schema_version":1,"prompt_version":1,"projection_version":1,"facts":[{"text":"Rome","source_seqs":[4]}],"unresolved_instructions":[],"l0_manifest_hash":"` + strings.Repeat("a", 64) + `","source_tokens":1000,"summary_tokens":200}`)
}

func TestSummarizeStructuredSucceeds(t *testing.T) {
	gen := &fakeSummaryGenerator{out: validSummaryJSON()}
	got, err := SummarizeStructured(t.Context(), gen, []byte("prompt"), SummaryValidation{ExpectedL0Hash: strings.Repeat("a", 64), MaxSummaryTokens: 400, MinReductionPercent: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || len(got.Facts) != 1 {
		t.Fatalf("got=%+v", got)
	}
}

func TestSummarizeStructuredPropagatesGeneratorError(t *testing.T) {
	wantErr := errors.New("inference unavailable")
	gen := &fakeSummaryGenerator{err: wantErr}
	if _, err := SummarizeStructured(t.Context(), gen, []byte("prompt"), SummaryValidation{}); !errors.Is(err, wantErr) {
		t.Fatalf("err=%v", err)
	}
}

func TestSummarizeStructuredPropagatesParseError(t *testing.T) {
	gen := &fakeSummaryGenerator{out: []byte("{not-json")}
	if _, err := SummarizeStructured(t.Context(), gen, []byte("prompt"), SummaryValidation{}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSummarizeStructuredPropagatesValidationError(t *testing.T) {
	gen := &fakeSummaryGenerator{out: validSummaryJSON()}
	if _, err := SummarizeStructured(t.Context(), gen, []byte("prompt"), SummaryValidation{ExpectedL0Hash: "mismatch", MaxSummaryTokens: 400, MinReductionPercent: 50}); err == nil {
		t.Fatal("expected validation error")
	}
}
