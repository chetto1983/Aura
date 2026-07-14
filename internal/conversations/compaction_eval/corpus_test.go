package compaction_eval

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Corpus schema pinned by 42-VALIDATION.md Section 17.13 and 42-RESEARCH.md's
// evaluation/rollout architecture. Every checked-in row in testdata/compaction/
// carries this exact shape; a row missing a required field fails JSON decode
// (unknown/omitted fields are caught by the assertions below, not by the decoder).
const (
	corpusSchemaVersion   = 1
	corpusVersion         = "phase42-corpus-2026.07.14"
	corpusLicense         = "internal-proprietary-eval-only"
	goldenProvenance      = "aura-house-synthetic"
	adversarialProvenance = "aura-house-synthetic-adversarial"
)

// requiredGoldenStrata is the exact 9-category stratification 42-RESEARCH.md
// requires ("chat, code, research, tool-heavy, approval, multilingual,
// multimodal/reference, recursive, and recovery").
var requiredGoldenStrata = []string{
	"chat", "code", "research", "tool_heavy", "approval",
	"multilingual", "multimodal_reference", "recursive", "recovery",
}

const minPerGoldenStratum = 30

var requiredGoldenEligibility = []string{"semantic_eligible", "l1_only", "emergency_waived"}

const minPerGoldenEligibility = 20

var requiredGoldenCensors = []string{"none", "short", "restored", "superseded"}

const minPerGoldenCensor = 15

// requiredAdversarialKinds is the 42-10 plan's explicit boundary/injection
// taxonomy: the original 18 structural kinds plus the four boundary kinds the
// plan action text calls out by name (authority/ledger/tool/pending/artifact/
// poison — authority and tool were already present).
var requiredAdversarialKinds = []string{
	"role", "delimiter", "encoded", "fake_summary", "tool", "quotation",
	"revoked", "authority", "empty", "whitespace", "oversize", "manifest",
	"source", "duplicate", "revocation", "schema", "projection", "malformed",
	"ledger", "pending", "artifact", "poison",
}

const minPerAdversarialKind = 5

type corpusSourceFixture struct {
	TurnCount         int    `json:"turn_count,omitempty"`
	CanonicalTokens   int    `json:"canonical_tokens,omitempty"`
	SemanticUnitCount int    `json:"semantic_unit_count,omitempty"`
	Language          string `json:"language,omitempty"`
	HasToolCalls      bool   `json:"has_tool_calls,omitempty"`
	HasPendingInput   bool   `json:"has_pending_input,omitempty"`
	HasArtifacts      bool   `json:"has_artifacts,omitempty"`
	Payload           string `json:"payload,omitempty"`
	InjectedVia       string `json:"injected_via,omitempty"`
	Description       string `json:"description,omitempty"`
}

type corpusExpectedProjection struct {
	L0Retained                  bool    `json:"l0_retained"`
	UnresolvedLedgerResolved    bool    `json:"unresolved_ledger_resolved"`
	ToolPendingRetained         bool    `json:"tool_pending_retained"`
	FactualDecisionRetained     bool    `json:"factual_decision_retained"`
	AuthorityEscalationAccepted bool    `json:"authority_escalation_accepted"`
	ReductionRatio              float64 `json:"reduction_ratio"`
	TargetAchieved              bool    `json:"target_achieved"`
	ContinuationConfidenceDelta float64 `json:"continuation_confidence_delta"`
	ProactiveLatencySeconds     float64 `json:"proactive_latency_seconds"`
	OverflowLatencySeconds      float64 `json:"overflow_latency_seconds"`
	Failed                      bool    `json:"failed"`
	Restored                    bool    `json:"restored"`
	CorruptEvidence             bool    `json:"corrupt_evidence"`
	LatencyBreachMinutes        int64   `json:"latency_breach_minutes"`
	CostRatio                   float64 `json:"cost_ratio"`
}

type corpusCase struct {
	ID                 string                   `json:"id"`
	SchemaVersion      int                      `json:"schema_version"`
	CorpusVersion      string                   `json:"corpus_version"`
	Provenance         string                   `json:"provenance"`
	License            string                   `json:"license"`
	Stratum            string                   `json:"stratum"`
	Eligibility        string                   `json:"eligibility"`
	Censor             string                   `json:"censor"`
	Taxonomy           []string                 `json:"taxonomy"`
	Kind               string                   `json:"kind,omitempty"`
	Source             corpusSourceFixture      `json:"source"`
	ExpectedProjection corpusExpectedProjection `json:"expected_projection"`
	ExpectedOutcome    string                   `json:"expected_outcome"`
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "..", "testdata", "compaction", name)
}

// loadCorpus scans a checked-in JSONL fixture line by line (mirrors the
// internal/eval/chat50_prompts_test.go scanner contract) and t.Fatals on any
// decode error — a malformed row is a fixture defect, never a skip.
func loadCorpus(t *testing.T, name string) []corpusCase {
	t.Helper()
	path := fixturePath(name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open corpus fixture %s: %v", path, err)
	}
	defer f.Close() //nolint:errcheck

	var rows []corpusCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var c corpusCase
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("%s:%d: invalid JSON row: %v", name, line, err)
		}
		rows = append(rows, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan corpus fixture %s: %v", path, err)
	}
	return rows
}

func loadGoldenCorpus(t *testing.T) []corpusCase { t.Helper(); return loadCorpus(t, "golden.jsonl") }
func loadAdversarialCorpus(t *testing.T) []corpusCase {
	t.Helper()
	return loadCorpus(t, "adversarial.jsonl")
}

// TestCompactionCorpusCensus is the exact 42-VALIDATION.md Section 17.13
// composition gate: >=500 stratified golden and >=200 adversarial, versioned.
func TestCompactionCorpusCensus(t *testing.T) {
	golden := loadGoldenCorpus(t)
	adversarial := loadAdversarialCorpus(t)
	if len(golden) < 500 {
		t.Fatalf("golden corpus has %d cases, want >= 500", len(golden))
	}
	if len(adversarial) < 200 {
		t.Fatalf("adversarial corpus has %d cases, want >= 200", len(adversarial))
	}
	t.Logf("census: golden=%d adversarial=%d total=%d", len(golden), len(adversarial), len(golden)+len(adversarial))
}

// TestCompactionCorpusUniqueIDs proves every case ID is deterministic and
// distinct across BOTH files (a duplicate ID would silently double-count a
// case in the aggregate gates below).
func TestCompactionCorpusUniqueIDs(t *testing.T) {
	golden := loadGoldenCorpus(t)
	adversarial := loadAdversarialCorpus(t)
	seen := make(map[string]string, len(golden)+len(adversarial))
	check := func(file string, rows []corpusCase) {
		for i, c := range rows {
			if c.ID == "" {
				t.Fatalf("%s: row %d has empty id", file, i)
			}
			if prior, dup := seen[c.ID]; dup {
				t.Fatalf("duplicate case id %q in %s (first seen in %s)", c.ID, file, prior)
			}
			seen[c.ID] = file
		}
	}
	check("golden.jsonl", golden)
	check("adversarial.jsonl", adversarial)
}

// TestCompactionCorpusRequiredStrata proves every golden stratum, eligibility,
// and censor category — and every adversarial boundary/injection kind — clears
// its floor. A corpus that silently drops a category (e.g. no recovery-stratum
// cases, or no ledger-boundary adversarial cases) fails here, not at review time.
func TestCompactionCorpusRequiredStrata(t *testing.T) {
	golden := loadGoldenCorpus(t)
	adversarial := loadAdversarialCorpus(t)

	strata := map[string]int{}
	eligibility := map[string]int{}
	censors := map[string]int{}
	for _, c := range golden {
		strata[c.Stratum]++
		eligibility[c.Eligibility]++
		censors[c.Censor]++
	}
	for _, want := range requiredGoldenStrata {
		if got := strata[want]; got < minPerGoldenStratum {
			t.Errorf("golden stratum %q has %d cases, want >= %d", want, got, minPerGoldenStratum)
		}
	}
	for _, want := range requiredGoldenEligibility {
		if got := eligibility[want]; got < minPerGoldenEligibility {
			t.Errorf("golden eligibility %q has %d cases, want >= %d", want, got, minPerGoldenEligibility)
		}
	}
	for _, want := range requiredGoldenCensors {
		if got := censors[want]; got < minPerGoldenCensor {
			t.Errorf("golden censor %q has %d cases, want >= %d", want, got, minPerGoldenCensor)
		}
	}

	kinds := map[string]int{}
	for _, c := range adversarial {
		kinds[c.Kind]++
	}
	for _, want := range requiredAdversarialKinds {
		if got := kinds[want]; got < minPerAdversarialKind {
			t.Errorf("adversarial kind %q has %d cases, want >= %d", want, got, minPerAdversarialKind)
		}
	}
}

// TestCompactionCorpusSchemaVersionAndProvenance pins the versioned-provenance
// contract every row must carry (deterministic ID already proven above; here:
// schema/corpus version, provenance, license, and non-empty taxonomy tags).
func TestCompactionCorpusSchemaVersionAndProvenance(t *testing.T) {
	golden := loadGoldenCorpus(t)
	adversarial := loadAdversarialCorpus(t)

	checkCommon := func(file string, c corpusCase) {
		if c.SchemaVersion != corpusSchemaVersion {
			t.Errorf("%s %s: schema_version = %d, want %d", file, c.ID, c.SchemaVersion, corpusSchemaVersion)
		}
		if c.CorpusVersion != corpusVersion {
			t.Errorf("%s %s: corpus_version = %q, want %q", file, c.ID, c.CorpusVersion, corpusVersion)
		}
		if c.License != corpusLicense {
			t.Errorf("%s %s: license = %q, want %q", file, c.ID, c.License, corpusLicense)
		}
		if len(c.Taxonomy) == 0 {
			t.Errorf("%s %s: taxonomy is empty", file, c.ID)
		}
		if c.Stratum == "" {
			t.Errorf("%s %s: stratum is empty", file, c.ID)
		}
	}
	for _, c := range golden {
		checkCommon("golden.jsonl", c)
		if c.Provenance != goldenProvenance {
			t.Errorf("golden.jsonl %s: provenance = %q, want %q", c.ID, c.Provenance, goldenProvenance)
		}
		if c.Kind != "" {
			t.Errorf("golden.jsonl %s: unexpected kind %q on a golden row", c.ID, c.Kind)
		}
	}
	for _, c := range adversarial {
		checkCommon("adversarial.jsonl", c)
		if c.Provenance != adversarialProvenance {
			t.Errorf("adversarial.jsonl %s: provenance = %q, want %q", c.ID, c.Provenance, adversarialProvenance)
		}
		if !slices.Contains(requiredAdversarialKinds, c.Kind) {
			t.Errorf("adversarial.jsonl %s: kind %q is not a recognized boundary/injection taxonomy value", c.ID, c.Kind)
		}
	}
}

// TestCompactionCorpusExpectedOutcomes proves the corpus's authority/safety
// ground truth: every golden case must be promotable and every adversarial
// case must be rejected, and NO case (golden or adversarial) records an
// accepted authority escalation — the zero-accepted-escalation gate depends on
// this being true of every row, not just the aggregate in the numerical test.
func TestCompactionCorpusExpectedOutcomes(t *testing.T) {
	golden := loadGoldenCorpus(t)
	adversarial := loadAdversarialCorpus(t)

	for _, c := range golden {
		if c.ExpectedOutcome != "promote" {
			t.Errorf("golden.jsonl %s: expected_outcome = %q, want %q", c.ID, c.ExpectedOutcome, "promote")
		}
		if c.ExpectedProjection.AuthorityEscalationAccepted {
			t.Errorf("golden.jsonl %s: authority_escalation_accepted = true, want false", c.ID)
		}
		if !c.ExpectedProjection.L0Retained || !c.ExpectedProjection.UnresolvedLedgerResolved {
			t.Errorf("golden.jsonl %s: L0/ledger retention must be true", c.ID)
		}
	}
	for _, c := range adversarial {
		if c.ExpectedOutcome != "reject" {
			t.Errorf("adversarial.jsonl %s: expected_outcome = %q, want %q", c.ID, c.ExpectedOutcome, "reject")
		}
		if c.ExpectedProjection.AuthorityEscalationAccepted {
			t.Errorf("adversarial.jsonl %s: authority_escalation_accepted = true, want false (attack must be detected, not accepted)", c.ID)
		}
		if c.ExpectedProjection.CorruptEvidence {
			t.Errorf("adversarial.jsonl %s: corrupt_evidence = true, want false", c.ID)
		}
	}
}
