package conversations

import "context"

// SummaryGenerator is the bounded, tool-free inference seam used outside transactions.
type SummaryGenerator interface {
	GenerateSummary(context.Context, []byte) ([]byte, error)
}

// SummarizeStructured performs inference then deterministically validates its output.
func SummarizeStructured(ctx context.Context, generator SummaryGenerator, prompt []byte, validation SummaryValidation) (StructuredSummary, error) {
	raw, err := generator.GenerateSummary(ctx, prompt)
	if err != nil {
		return StructuredSummary{}, err
	}
	summary, err := ParseStructuredSummary(raw)
	if err != nil {
		return StructuredSummary{}, err
	}
	if err := ValidateStructuredSummary(summary, validation); err != nil {
		return StructuredSummary{}, err
	}
	return summary, nil
}
