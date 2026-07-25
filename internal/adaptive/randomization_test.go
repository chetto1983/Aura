package adaptive

import (
	"bytes"
	"testing"
	"time"
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
