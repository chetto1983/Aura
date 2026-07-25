package adaptive

// OutcomeDisposition is the terminal state of one sealed look.
type OutcomeDisposition string

const (
	OutcomeDispositionContinue     OutcomeDisposition = "continue"
	OutcomeDispositionPromote      OutcomeDisposition = "promote"
	OutcomeDispositionRetainCanary OutcomeDisposition = "retain_canary"
	OutcomeDispositionIneligible   OutcomeDisposition = "ineligible"
)

// DispositionInput contains only gate-authoritative values; OPE is intentionally absent.
type DispositionInput struct {
	PreconditionFailure  bool
	LookNumber           int
	GateValid            bool
	QualityLower         ExactRational
	MinimumQualityUplift ExactRational
	HarmUpper            ExactRational
	MaximumHarmIncrease  ExactRational
}

// DispositionDecision states whether and how the sole writer may seal a look.
type DispositionDecision struct {
	Write       bool
	Eligible    bool
	Disposition OutcomeDisposition
}

// DecideOutcomeDisposition applies the frozen ordered truth table.
func DecideOutcomeDisposition(input DispositionInput) DispositionDecision {
	if input.PreconditionFailure || input.LookNumber < 1 || input.LookNumber > 5 {
		return DispositionDecision{}
	}
	if !input.GateValid {
		return DispositionDecision{Write: true, Disposition: OutcomeDispositionIneligible}
	}
	qualityPass := input.QualityLower.Cmp(input.MinimumQualityUplift) > 0
	harmPass := input.HarmUpper.Cmp(input.MaximumHarmIncrease) <= 0
	if qualityPass && harmPass {
		return DispositionDecision{
			Write: true, Eligible: true, Disposition: OutcomeDispositionPromote,
		}
	}
	if input.LookNumber < 5 {
		return DispositionDecision{
			Write: true, Eligible: true, Disposition: OutcomeDispositionContinue,
		}
	}
	return DispositionDecision{
		Write: true, Eligible: true, Disposition: OutcomeDispositionRetainCanary,
	}
}
