package mediagen

import (
	"math"
	"slices"
	"strings"
	"testing"
)

func TestNearestIntKeepsClosestValueAndLowerTie(t *testing.T) {
	cases := []struct {
		name      string
		want      int
		supported []int
		expected  int
	}{
		{
			name:      "negative request uses absolute distance",
			want:      -10,
			supported: []int{-5, 10},
			expected:  -5,
		},
		{
			name:      "tie keeps an earlier lower value",
			want:      7,
			supported: []int{5, 9},
			expected:  5,
		},
		{
			name:      "tie replaces an earlier higher value",
			want:      7,
			supported: []int{9, 5},
			expected:  5,
		},
		{
			name:      "closer higher value wins",
			want:      8,
			supported: []int{5, 10},
			expected:  10,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nearestInt(tc.want, tc.supported); got != tc.expected {
				t.Fatalf("nearestInt(%d, %v) = %d, want %d", tc.want, tc.supported, got, tc.expected)
			}
		})
	}
}

func TestResolutionHeightRejectsMalformedAndNonpositiveValues(t *testing.T) {
	cases := []struct {
		label    string
		height   float64
		measured bool
	}{
		{label: " 2K ", height: 2048, measured: true},
		{label: "4k", height: 4096, measured: true},
		{label: "512p", height: 512, measured: true},
		{label: "512", measured: false},
		{label: "0p", measured: false},
		{label: "-1p", measured: false},
		{label: "cinemap", measured: false},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, measured := resolutionHeight(tc.label)
			if got != tc.height || measured != tc.measured {
				t.Fatalf("resolutionHeight(%q) = (%v, %t), want (%v, %t)", tc.label, got, measured, tc.height, tc.measured)
			}
		})
	}
}

func TestAspectRatioValueRejectsInvalidDimensions(t *testing.T) {
	cases := []struct {
		label    string
		value    float64
		measured bool
	}{
		{label: "16:9", value: 16.0 / 9.0, measured: true},
		{label: "1", measured: false},
		{label: "wide:1", measured: false},
		{label: "1:tall", measured: false},
		{label: "0:1", measured: false},
		{label: "1:0", measured: false},
		{label: "-1:1", measured: false},
		{label: "1:-1", measured: false},
		{label: "+Inf:1", measured: false},
		{label: "1:+Inf", measured: false},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, measured := aspectRatioValue(tc.label)
			if measured != tc.measured || math.Abs(got-tc.value) > 1e-12 {
				t.Fatalf("aspectRatioValue(%q) = (%v, %t), want (%v, %t)", tc.label, got, measured, tc.value, tc.measured)
			}
		})
	}
}

func TestClampReferenceLimitsDefendAgainstInvalidCatalogValues(t *testing.T) {
	negative := -1
	refs := []string{"r1", "r2"}
	// A negative maximum reads as zero: the model takes no references, so both refuse.
	if _, _, err := ClampImage(ImageInput{ReferenceAssetIDs: refs}, &Model{
		Parameters: map[string]Parameter{"input_references": {Max: &negative}},
	}); ErrorCode(err) != "unsupported" {
		t.Fatalf("image with a negative maximum: err = %v, want unsupported", err)
	}
	if _, _, err := ClampVideo(VideoInput{ReferenceAssetIDs: refs}, &Model{
		Parameters: map[string]Parameter{"input_references": {Max: &negative}},
	}); ErrorCode(err) != "unsupported" {
		t.Fatalf("video with a negative maximum: err = %v, want unsupported", err)
	}

	video, _, err := ClampVideo(VideoInput{ReferenceAssetIDs: refs}, &Model{
		Parameters: map[string]Parameter{"input_references": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(video.ReferenceAssetIDs, refs) {
		t.Fatalf("video references = %q, want %q", video.ReferenceAssetIDs, refs)
	}
}

func TestAdjustmentsChooseKeepsTheFirstEqualCandidate(t *testing.T) {
	cases := []struct {
		name      string
		want      string
		supported []string
		values    map[string]float64
		expected  string
	}{
		{
			name:      "lower numeric tie is retained",
			want:      "7",
			supported: []string{"5", "9"},
			values:    map[string]float64{"7": 7, "5": 5, "9": 9},
			expected:  "5",
		},
		{
			name:      "equal measurements retain first catalog value",
			want:      "target",
			supported: []string{"first", "second"},
			values:    map[string]float64{"target": 0, "first": 1, "second": 1},
			expected:  "first",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var notes adjustments
			got := notes.choose("setting", tc.want, tc.supported, func(value string) (float64, bool) {
				measured, ok := tc.values[value]
				return measured, ok
			})
			if got != tc.expected {
				t.Fatalf("choose(%q, %q) = %q, want %q", tc.want, tc.supported, got, tc.expected)
			}
			wantNotes := []string{"setting " + tc.want + " is not offered; used " + tc.expected}
			if !slices.Equal(notes, wantNotes) {
				t.Fatalf("notes = %q, want %q", notes, wantNotes)
			}
		})
	}
}

func TestClampImageTable(t *testing.T) {
	mai := Model{
		ID: "microsoft/mai-image-2.6", Kind: KindImage,
		Parameters: map[string]Parameter{
			"aspect_ratio":     {Type: "enum", Values: []string{"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3", "auto"}},
			"n":                {Type: "range", Min: new(1), Max: new(1)},
			"input_references": {Type: "range", Min: new(0), Max: new(5)},
		},
	}
	refs := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7"}
	cases := []struct {
		name      string
		in        ImageInput
		model     *Model
		want      ImageInput
		notes     []string
		errorCode string
	}{
		{
			name:  "nil model passes every input through",
			in:    ImageInput{Prompt: "p", AspectRatio: "21:9", ReferenceAssetIDs: refs},
			model: nil,
			want:  ImageInput{Prompt: "p", AspectRatio: "21:9", ReferenceAssetIDs: refs},
		},
		{
			name:  "declared ratio and references within the maximum stay unchanged",
			in:    ImageInput{Prompt: "p", AspectRatio: "3:2", ReferenceAssetIDs: refs[:5]},
			model: &mai,
			want:  ImageInput{Prompt: "p", AspectRatio: "3:2", ReferenceAssetIDs: refs[:5]},
		},
		{
			name:  "undeclared ratio clamps to the nearest declared one",
			in:    ImageInput{Prompt: "p", AspectRatio: "21:9"},
			model: &mai,
			want:  ImageInput{Prompt: "p", AspectRatio: "16:9"},
			notes: []string{"aspect ratio 21:9 is not offered; used 16:9"},
		},
		{
			name:      "references beyond the declared maximum are refused",
			in:        ImageInput{Prompt: "p", ReferenceAssetIDs: refs},
			model:     &mai,
			errorCode: "unsupported",
		},
		{
			name:  "a model without descriptors drops the ratio",
			in:    ImageInput{Prompt: "p", AspectRatio: "1:1"},
			model: &Model{ID: "text-only"},
			want:  ImageInput{Prompt: "p"},
			notes: []string{"aspect ratio 1:1 is not supported; omitted"},
		},
		{
			name:      "an edit on a model declaring no references is refused",
			in:        ImageInput{Prompt: "p", AspectRatio: "1:1", ReferenceAssetIDs: refs[:2]},
			model:     &Model{ID: "text-only"},
			errorCode: "unsupported",
		},
		{
			name:  "a ratio descriptor declaring only auto drops the ratio",
			in:    ImageInput{Prompt: "p", AspectRatio: "4:3"},
			model: &Model{Parameters: map[string]Parameter{"aspect_ratio": {Type: "enum", Values: []string{"auto"}}}},
			want:  ImageInput{Prompt: "p"},
			notes: []string{"aspect ratio 4:3 is not supported; omitted"},
		},
		{
			name:      "a zero reference maximum refuses any reference",
			in:        ImageInput{Prompt: "p", ReferenceAssetIDs: refs[:1]},
			model:     &Model{Parameters: map[string]Parameter{"input_references": {Type: "range", Min: new(0), Max: new(0)}}},
			errorCode: "unsupported",
		},
		{
			name:  "a reference descriptor without a maximum keeps every reference",
			in:    ImageInput{Prompt: "p", ReferenceAssetIDs: refs},
			model: &Model{Parameters: map[string]Parameter{"input_references": {Type: "range"}}},
			want:  ImageInput{Prompt: "p", ReferenceAssetIDs: refs},
		},
		{
			name:  "no requested ratio or references produces no note",
			in:    ImageInput{Prompt: "p"},
			model: &Model{ID: "text-only"},
			want:  ImageInput{Prompt: "p"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			used, notes, err := ClampImage(tc.in, tc.model)
			if tc.errorCode != "" {
				if ErrorCode(err) != tc.errorCode || notes != nil || used.Prompt != "" {
					t.Fatalf("err = %v, used = %#v, notes = %q; want %s refusal", err, used, notes, tc.errorCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if used.Prompt != tc.want.Prompt || used.AspectRatio != tc.want.AspectRatio ||
				!slices.Equal(used.ReferenceAssetIDs, tc.want.ReferenceAssetIDs) {
				t.Fatalf("used = %#v, want %#v", used, tc.want)
			}
			if !slices.Equal(notes, tc.notes) {
				t.Fatalf("notes = %q, want %q", notes, tc.notes)
			}
		})
	}
}

func TestClampNeverAliasesCallerInput(t *testing.T) {
	refs := []string{"r1", "r2", "r3"}
	image, _, _ := ClampImage(ImageInput{ReferenceAssetIDs: refs}, nil)
	image.ReferenceAssetIDs[0] = "changed"
	bounded, _, err := ClampImage(ImageInput{ReferenceAssetIDs: refs}, &Model{
		Parameters: map[string]Parameter{"input_references": {Type: "range", Max: new(3)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bounded.ReferenceAssetIDs[2] = "changed"
	bounded.ReferenceAssetIDs = append(bounded.ReferenceAssetIDs, "appended")

	audio, seed := true, 7
	video, _, err := ClampVideo(VideoInput{ReferenceAssetIDs: refs, Audio: &audio, Seed: &seed}, &Model{GenerateAudio: true, Seed: true})
	if err != nil {
		t.Fatal(err)
	}
	video.ReferenceAssetIDs[1] = "changed"
	*video.Audio = false
	*video.Seed = 0
	passthrough, _, _ := ClampVideo(VideoInput{Audio: &audio, Seed: &seed}, nil)
	*passthrough.Audio = false
	*passthrough.Seed = 0

	if !slices.Equal(refs, []string{"r1", "r2", "r3"}) || !audio || seed != 7 {
		t.Fatalf("caller input mutated: refs=%q audio=%v seed=%d", refs, audio, seed)
	}
}

// TestClampRefusalsTellTheModelWhatToDo pins the wording the model acts on. A reference the
// model cannot take used to be dropped while the call still went out and was billed: an edit
// silently turned into a fresh image, found in review on 2026-09-16. Each refusal now says
// nothing was generated and what to do instead, so it works without the media skill loaded.
func TestClampRefusalsTellTheModelWhatToDo(t *testing.T) {
	noRefs := &Model{ID: "text-only"}
	twoRefs := &Model{Parameters: map[string]Parameter{"input_references": {Type: "range", Max: new(2)}}}
	three := []string{"a", "b", "c"}
	for _, tc := range []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "image model without references",
			err:  imageErr(ClampImage(ImageInput{Prompt: "p", ReferenceAssetIDs: three[:1]}, noRefs)),
			want: []string{"image model cannot use reference images", "Nothing was generated", "Do not retry without them", "accepts reference images"},
		},
		{
			name: "image model with a lower maximum",
			err:  imageErr(ClampImage(ImageInput{Prompt: "p", ReferenceAssetIDs: three}, twoRefs)),
			want: []string{"image model accepts at most 2 reference images and 3 were given", "Nothing was generated", "the 2 that matter most"},
		},
		{
			name: "video model with a lower maximum",
			err:  videoErr(ClampVideo(VideoInput{Prompt: "p", ReferenceAssetIDs: three}, twoRefs)),
			want: []string{"video model accepts at most 2 reference images and 3 were given", "Nothing was generated"},
		},
		{
			name: "video model without image-to-video",
			err:  videoErr(ClampVideo(VideoInput{Prompt: "p", FirstFrameAssetID: "frame"}, noRefs)),
			want: []string{"cannot start from an image", "Nothing was generated", "Do not resubmit it as a text-only video", "image-to-video"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ErrorCode(tc.err) != "unsupported" {
				t.Fatalf("err = %v, want unsupported", tc.err)
			}
			for _, part := range tc.want {
				if !strings.Contains(tc.err.Error(), part) {
					t.Fatalf("message %q lacks %q", tc.err.Error(), part)
				}
			}
		})
	}
	if _, _, err := ClampImage(ImageInput{Prompt: "p"}, noRefs); err != nil {
		t.Fatalf("a plain generation on a model without references was refused: %v", err)
	}
}

func imageErr(_ ImageInput, _ []string, err error) error { return err }
