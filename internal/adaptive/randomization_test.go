package adaptive

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMapRandomizationByte_SelectsFrozenArmFromLowBit(t *testing.T) {
	tests := []struct {
		draw byte
		arm  RandomizedArm
	}{
		{draw: 0x00, arm: RandomizedArmBaseline},
		{draw: 0x01, arm: RandomizedArmChallenger},
		{draw: 0xfe, arm: RandomizedArmBaseline},
		{draw: 0xff, arm: RandomizedArmChallenger},
	}
	for _, test := range tests {
		if got := MapRandomizationByte(test.draw); got != test.arm {
			t.Fatalf("MapRandomizationByte(%02x) = %q, want %q", test.draw, got, test.arm)
		}
	}
}

func TestRandomizationPrimitives_RejectMalformedInputs(t *testing.T) {
	if _, err := drawRandomizedArm(nil); err == nil {
		t.Fatal("drawRandomizedArm() accepted nil entropy")
	}
	if _, err := drawRandomizedArm(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("drawRandomizedArm() error = %v", err)
	}
	tests := []struct {
		catalog    []string
		champion   string
		challenger string
	}{
		{catalog: nil, champion: "static", challenger: "static"},
		{catalog: []string{"static"}, champion: "other", challenger: "static"},
		{catalog: []string{"static"}, champion: "static", challenger: "other"},
		{catalog: []string{"static", "static"}, champion: "static", challenger: "static"},
	}
	for _, test := range tests {
		if _, err := MarginalActionProbabilities(
			test.catalog, test.champion, test.challenger,
		); err == nil {
			t.Fatalf("MarginalActionProbabilities(%v) accepted malformed input", test.catalog)
		}
	}
	if _, err := NewAnalysisStratum(
		"invalid", time.Now(), 60,
	); err == nil {
		t.Fatal("NewAnalysisStratum() accepted invalid owner")
	}
	if _, err := NewAnalysisStratum(
		"11111111-1111-4111-8111-111111111111", time.Unix(-1, 0), 60,
	); err == nil {
		t.Fatal("NewAnalysisStratum() accepted pre-origin time")
	}
	if _, err := NewInterferenceCluster(
		"invalid", "22222222-2222-4222-8222-222222222222",
	); err == nil {
		t.Fatal("NewInterferenceCluster() accepted invalid owner")
	}
	if _, err := NewInterferenceCluster(
		"11111111-1111-4111-8111-111111111111", "invalid",
	); err == nil {
		t.Fatal("NewInterferenceCluster() accepted invalid conversation")
	}
}

func TestDrawRandomizedArm_ReadsExactlyOneByte_When_EntropyIsAvailable(t *testing.T) {
	source := &countingReader{Reader: bytes.NewReader([]byte{0xff, 0x00})}
	selection, err := drawRandomizedArm(source)
	if err != nil {
		t.Fatalf("drawRandomizedArm() error = %v", err)
	}
	if selection.Arm != RandomizedArmChallenger || selection.DrawByte != "ff" || selection.MappedBit != 1 {
		t.Fatalf("selection = %#v", selection)
	}
	if source.bytesRead != 1 {
		t.Fatalf("entropy bytes read = %d, want 1", source.bytesRead)
	}
}

func TestDrawRandomizedArm_UsesProductionEntropyBoundary(t *testing.T) {
	selection, err := DrawRandomizedArm()
	if err != nil {
		t.Fatalf("DrawRandomizedArm() error = %v", err)
	}
	if len(selection.DrawByte) != 2 ||
		selection.MappedBit > 1 ||
		selection.Arm != MapRandomizationByte(selection.MappedBit) {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestMarginalActionProbabilities_AggregatesCoincidentArmActions(t *testing.T) {
	got, err := MarginalActionProbabilities(
		[]string{"static", "summarize"},
		"static",
		"static",
	)
	if err != nil {
		t.Fatalf("MarginalActionProbabilities() error = %v", err)
	}
	want := []ExactActionProbability{
		{ActionID: "static", Probability: MustExactRational(1, 1)},
		{ActionID: "summarize", Probability: MustExactRational(0, 1)},
	}
	assertExactActionProbabilities(t, got, want)
}

func TestAnalysisIdentifiers_MatchFrozenGoldenHashes(t *testing.T) {
	ownerID := "11111111-1111-4111-8111-111111111111"
	claimedAt := time.Unix(60, 0).UTC()
	stratum, err := NewAnalysisStratum(ownerID, claimedAt, 60)
	if err != nil {
		t.Fatalf("NewAnalysisStratum() error = %v", err)
	}
	if stratum.ID != "620c5a36c88df7ce65c041d67d01754e3224b6e072624a2d6d8afa34b16bc942" {
		t.Fatalf("stratum ID = %s", stratum.ID)
	}
	cluster, err := NewInterferenceCluster(
		ownerID,
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("NewInterferenceCluster() error = %v", err)
	}
	if cluster.ID != "286a699a89345a049f7458ae5fa3986db1a17bac59ce407581e6f120cd41895e" {
		t.Fatalf("cluster ID = %s", cluster.ID)
	}
}

func TestNewAnalysisStratum_RejectsBlockWidthOverflow(t *testing.T) {
	_, err := NewAnalysisStratum(
		"11111111-1111-4111-8111-111111111111",
		time.Unix(60, 0).UTC(),
		math.MaxInt64,
	)
	if err == nil {
		t.Fatal("NewAnalysisStratum() accepted overflowing block width")
	}
}

func TestBuildRandomizationAlternatives_PrevalidatesBothArmsBeforeDraw(t *testing.T) {
	snapshotSpec := snapshotTestSpec()
	snapshot, err := NewPolicySnapshot(snapshotSpec)
	if err != nil {
		t.Fatal(err)
	}
	cohortSpec := focalCohortTestSpec()
	cohortSpec.Scope = CohortScope{
		OwnerID:        snapshot.Scope().OwnerID,
		ProviderID:     snapshot.Scope().ProviderID,
		ModelID:        snapshot.Scope().ModelID,
		PolicyVersion:  snapshot.Scope().PolicyVersion,
		PolicyEpoch:    7,
		SnapshotID:     snapshot.ID(),
		SnapshotSHA256: snapshot.SHA256(),
		Environment:    EvaluationProductionCanary,
	}
	cohortSpec.Predicate = FocalPredicate{
		Domain: snapshot.Scope().Domain, Point: snapshot.Scope().Point, Ordinal: 1,
	}
	cohortSpec.Actions = []ActionProbability{
		{ActionID: "deep", Probability: 0.25},
		{ActionID: "direct", Probability: 0.25},
		{ActionID: StaticActionID, Probability: 0.5},
	}
	cohort := mustFocalCohort(t, cohortSpec)
	request := FocalEnrollmentRequest{
		OwnerID: cohort.Scope().OwnerID, CohortID: cohort.ID(),
		RequestID:                uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		EvaluationConversationID: uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		Domain:                   cohort.Domain(), Point: cohort.Predicate().Point,
		PointOrdinal: cohort.Predicate().Ordinal,
	}
	assignmentID, err := request.assignmentID()
	if err != nil {
		t.Fatal(err)
	}
	claim := FocalClaim{
		ID:      uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		OwnerID: request.OwnerID, CohortID: request.CohortID,
		RequestID: request.RequestID, EvaluationConversationID: request.EvaluationConversationID,
		AssignmentID: assignmentID, Domain: request.Domain, Point: request.Point,
		PointOrdinal: request.PointOrdinal, ClaimedAt: time.Unix(60, 0).UTC(),
	}
	template := AssignmentTemplate{Features: []SnapshotFeatureValue{
		{Key: FeatureCandidateCount, Value: 2},
		{Key: FeatureContextLength, Value: 10},
	}}

	alternatives, err := buildRandomizationAlternatives(cohort, snapshot, request, claim, template)
	if err != nil {
		t.Fatalf("buildRandomizationAlternatives() error = %v", err)
	}
	if alternatives.baseline.ArmID != "baseline" ||
		alternatives.baseline.IntendedActionID != StaticActionID ||
		alternatives.challenger.ArmID != "challenger" ||
		alternatives.challenger.IntendedActionID != "deep" {
		t.Fatalf("alternatives = %#v", alternatives)
	}
	if alternatives.baseline.RecommendedActionID != "deep" ||
		alternatives.challenger.RecommendedActionID != "deep" {
		t.Fatalf("recommended actions = %q/%q", alternatives.baseline.RecommendedActionID, alternatives.challenger.RecommendedActionID)
	}
	if _, err := NewAssignmentEvent(alternatives.baseline); err != nil {
		t.Fatalf("baseline is not prevalidated: %v", err)
	}
	if _, err := NewAssignmentEvent(alternatives.challenger); err != nil {
		t.Fatalf("challenger is not prevalidated: %v", err)
	}
	if alternatives.receiptTemplate.RandomizationPlanSHA256 == "" {
		t.Fatal("randomization plan digest is empty")
	}
}

type countingReader struct {
	*bytes.Reader
	bytesRead int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	read, err := reader.Reader.Read(buffer)
	reader.bytesRead += read
	return read, err
}

func assertExactActionProbabilities(
	t *testing.T,
	got []ExactActionProbability,
	want []ExactActionProbability,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("probability count = %d, want %d", len(got), len(want))
	}
	for index := range got {
		if got[index].ActionID != want[index].ActionID ||
			got[index].Probability.Cmp(want[index].Probability) != 0 {
			t.Fatalf("probability[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}
