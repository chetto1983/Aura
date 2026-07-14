package conversations

import "testing"

func TestValidateAuthorityLedgerRejectionBranches(t *testing.T) {
	cases := []struct {
		name   string
		events []AuthorityLedgerEvent
	}{
		{"invalid source seq", []AuthorityLedgerEvent{{SourceSeq: 0, Authority: AuthorityUser, State: InstructionActive, QuotedData: "eA=="}}},
		{"system authority rejected", []AuthorityLedgerEvent{{SourceSeq: 1, Authority: AuthoritySystem, State: InstructionActive, QuotedData: "eA=="}}},
		{"duplicate source", []AuthorityLedgerEvent{
			{SourceSeq: 1, Authority: AuthorityUser, State: InstructionActive, QuotedData: "eA=="},
			{SourceSeq: 1, Authority: AuthorityUser, State: InstructionActive, QuotedData: "eQ=="},
		}},
		{"non-base64 quoted data", []AuthorityLedgerEvent{{SourceSeq: 1, Authority: AuthorityUser, State: InstructionActive, QuotedData: "not base64!!"}}},
		{"revocation missing source", []AuthorityLedgerEvent{{SourceSeq: 1, Authority: AuthorityUser, State: InstructionRevoked, RevokesSourceSeq: 0, QuotedData: "eA=="}}},
		{"revocation references unknown source", []AuthorityLedgerEvent{{SourceSeq: 1, Authority: AuthorityUser, State: InstructionRevoked, RevokesSourceSeq: 99, QuotedData: "eA=="}}},
		{"invalid instruction state", []AuthorityLedgerEvent{{SourceSeq: 1, Authority: AuthorityUser, State: "unknown", QuotedData: "eA=="}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateAuthorityLedger(tc.events); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestValidateAuthorityLedgerAcceptsToolAuthorityAndValidRevocation(t *testing.T) {
	events := []AuthorityLedgerEvent{
		{SourceSeq: 1, Authority: AuthorityTool, State: InstructionActive, QuotedData: "eA=="},
		{SourceSeq: 2, Authority: AuthorityUser, State: InstructionRevoked, RevokesSourceSeq: 1, QuotedData: "eQ=="},
	}
	if err := ValidateAuthorityLedger(events); err != nil {
		t.Fatalf("unexpected err=%v", err)
	}
}
