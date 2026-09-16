package mediagen

import (
	"math"
	"slices"
	"strings"
	"testing"
	"testing/quick"
)

func TestClampVideoUsesNearestDeclaredValues(t *testing.T) {
	muted := false
	in := VideoInput{Prompt: "moving sea", Duration: 4, Resolution: "720p", AspectRatio: "3:2", Audio: &muted}
	model := Model{
		Durations: []int{5, 10}, Resolutions: []string{"480p", "768p"},
		AspectRatios: []string{"16:9", "1:1"}, GenerateAudio: false,
	}
	used, changes, err := ClampVideo(in, &model)
	if err != nil {
		t.Fatal(err)
	}
	if used.Duration != 5 || used.Resolution != "768p" || used.AspectRatio != "16:9" ||
		used.Audio != nil || len(changes) != 4 {
		t.Fatalf("unexpected clamp: %#v, %v", used, changes)
	}
	want := []string{
		"duration 4s is not offered; used 5s",
		"resolution 720p is not offered; used 768p",
		"aspect ratio 3:2 is not offered; used 16:9",
		"audio false is not supported by this model; omitted",
	}
	if !slices.Equal(changes, want) {
		t.Fatalf("notes = %q, want %q", changes, want)
	}
}

func TestClampVideoDurationProperty(t *testing.T) {
	property := func(want int16, raw []int16) bool {
		supported := make([]int, 0, len(raw))
		for _, v := range raw {
			supported = append(supported, int(v))
		}
		if len(supported) == 0 || want == 0 {
			return true
		}
		used, _, err := ClampVideo(VideoInput{Duration: int(want)}, &Model{Durations: supported})
		if err != nil || !slices.Contains(supported, used.Duration) {
			return false
		}
		return !slices.Contains(supported, int(want)) || used.Duration == int(want)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatal(err)
	}
}

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

func TestClampVideoTable(t *testing.T) {
	hailuo := Model{
		ID: "minimax/hailuo-3-max", Kind: KindVideo,
		Durations:    []int{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		Resolutions:  []string{"768p", "480p"},
		AspectRatios: []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"},
		FrameImages:  []string{"first_frame", "last_frame"},
	}
	cases := []struct {
		name      string
		in        VideoInput
		model     *Model
		want      VideoInput
		notes     []string
		errorCode string
	}{
		{
			name:  "nil model passes every input through",
			in:    VideoInput{Prompt: "p", Duration: 99, Resolution: "4K", AspectRatio: "9:21", FirstFrameAssetID: "a", ReferenceAssetIDs: []string{"r"}, Audio: new(true)},
			model: nil,
			want:  VideoInput{Prompt: "p", Duration: 99, Resolution: "4K", AspectRatio: "9:21", FirstFrameAssetID: "a", ReferenceAssetIDs: []string{"r"}, Audio: new(true)},
		},
		{
			name:  "supported values stay unchanged",
			in:    VideoInput{Prompt: "p", Duration: 6, Resolution: "480p", AspectRatio: "9:16", FirstFrameAssetID: "frame"},
			model: &hailuo,
			want:  VideoInput{Prompt: "p", Duration: 6, Resolution: "480p", AspectRatio: "9:16", FirstFrameAssetID: "frame"},
		},
		{
			name:  "unset duration, resolution and ratio are not invented",
			in:    VideoInput{Prompt: "p"},
			model: &hailuo,
			want:  VideoInput{Prompt: "p"},
		},
		{
			name:      "first frame without first_frame support is refused",
			in:        VideoInput{Prompt: "p", FirstFrameAssetID: "frame"},
			model:     &Model{ID: "text-only", FrameImages: []string{"last_frame"}},
			errorCode: "unsupported",
		},
		{
			name:      "first frame on a model declaring no frames is refused",
			in:        VideoInput{Prompt: "p", FirstFrameAssetID: "frame"},
			model:     &Model{ID: "text-only"},
			errorCode: "unsupported",
		},
		{
			name:  "empty declared sets are left for provider validation",
			in:    VideoInput{Prompt: "p", Duration: 4, Resolution: "720p", AspectRatio: "3:2"},
			model: &Model{ID: "black-forest-labs/flux-video-edit"},
			want:  VideoInput{Prompt: "p", Duration: 4, Resolution: "720p", AspectRatio: "3:2"},
		},
		{
			name:  "nil audio produces no note",
			in:    VideoInput{Prompt: "p"},
			model: &Model{GenerateAudio: false},
			want:  VideoInput{Prompt: "p"},
		},
		{
			name:  "requested audio on a silent model is dropped",
			in:    VideoInput{Prompt: "p", Audio: new(true)},
			model: &Model{GenerateAudio: false},
			want:  VideoInput{Prompt: "p"},
			notes: []string{"audio true is not supported by this model; omitted"},
		},
		{
			name:  "audio false is kept when the model declares audio",
			in:    VideoInput{Prompt: "p", Audio: new(false)},
			model: &Model{GenerateAudio: true},
			want:  VideoInput{Prompt: "p", Audio: new(false)},
		},
		{
			name:  "duration tie picks the smaller value",
			in:    VideoInput{Duration: 7},
			model: &Model{Durations: []int{9, 5}},
			want:  VideoInput{Duration: 5},
			notes: []string{"duration 7s is not offered; used 5s"},
		},
		{
			name:  "negative duration clamps to the nearest declared value",
			in:    VideoInput{Duration: -3},
			model: &Model{Durations: []int{10, 5}},
			want:  VideoInput{Duration: 5},
			notes: []string{"duration -3s is not offered; used 5s"},
		},
		{
			name:  "resolution tie picks the smaller height",
			in:    VideoInput{Resolution: "720p"},
			model: &Model{Resolutions: []string{"800p", "640p"}},
			want:  VideoInput{Resolution: "640p"},
			notes: []string{"resolution 720p is not offered; used 640p"},
		},
		{
			name:  "K resolutions compare by pixel height",
			in:    VideoInput{Resolution: "2K"},
			model: &Model{Resolutions: []string{"4K", "1080p"}},
			want:  VideoInput{Resolution: "1080p"},
			notes: []string{"resolution 2K is not offered; used 1080p"},
		},
		{
			name:  "unparseable catalog resolutions are ignored",
			in:    VideoInput{Resolution: "1K"},
			model: &Model{Resolutions: []string{"cinema", "720p", "4k"}},
			want:  VideoInput{Resolution: "720p"},
			notes: []string{"resolution 1K is not offered; used 720p"},
		},
		{
			name:  "resolution with no comparable declared value is dropped",
			in:    VideoInput{Resolution: "1080p"},
			model: &Model{Resolutions: []string{"cinema"}},
			want:  VideoInput{},
			notes: []string{"resolution 1080p is not supported; omitted"},
		},
		{
			name:  "unparseable requested resolution is dropped",
			in:    VideoInput{Resolution: "huge"},
			model: &Model{Resolutions: []string{"720p"}},
			want:  VideoInput{},
			notes: []string{"resolution huge is not supported; omitted"},
		},
		{
			name:  "ratio tie picks the smaller ratio",
			in:    VideoInput{AspectRatio: "1:1"},
			model: &Model{AspectRatios: []string{"6:5", "4:5"}},
			want:  VideoInput{AspectRatio: "4:5"},
			notes: []string{"aspect ratio 1:1 is not offered; used 4:5"},
		},
		{
			name:  "auto is never measured as a ratio",
			in:    VideoInput{AspectRatio: "auto"},
			model: &Model{AspectRatios: []string{"16:9"}},
			want:  VideoInput{},
			notes: []string{"aspect ratio auto is not supported; omitted"},
		},
		{
			name:  "video references are retained without a declared maximum",
			in:    VideoInput{ReferenceAssetIDs: []string{"a", "b", "c"}},
			model: &hailuo,
			want:  VideoInput{ReferenceAssetIDs: []string{"a", "b", "c"}},
		},
		{
			name:      "more video references than the declared maximum are refused",
			in:        VideoInput{Prompt: "p", ReferenceAssetIDs: []string{"a", "b", "c"}},
			model:     &Model{Parameters: map[string]Parameter{"input_references": {Type: "range", Min: new(0), Max: new(2)}}},
			errorCode: "unsupported",
		},
		{
			name:  "video references up to the declared maximum are kept",
			in:    VideoInput{ReferenceAssetIDs: []string{"a", "b"}},
			model: &Model{Parameters: map[string]Parameter{"input_references": {Type: "range", Min: new(0), Max: new(2)}}},
			want:  VideoInput{ReferenceAssetIDs: []string{"a", "b"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			used, notes, err := ClampVideo(tc.in, tc.model)
			if tc.errorCode != "" {
				if ErrorCode(err) != tc.errorCode || notes != nil || used.Prompt != "" {
					t.Fatalf("err = %v, used = %#v, notes = %q; want %s refusal", err, used, notes, tc.errorCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertVideoInput(t, used, tc.want)
			if !slices.Equal(notes, tc.notes) {
				t.Fatalf("notes = %q, want %q", notes, tc.notes)
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

	audio := true
	video, _, err := ClampVideo(VideoInput{ReferenceAssetIDs: refs, Audio: &audio}, &Model{GenerateAudio: true})
	if err != nil {
		t.Fatal(err)
	}
	video.ReferenceAssetIDs[1] = "changed"
	*video.Audio = false
	passthrough, _, _ := ClampVideo(VideoInput{Audio: &audio}, nil)
	*passthrough.Audio = false

	if !slices.Equal(refs, []string{"r1", "r2", "r3"}) || !audio {
		t.Fatalf("caller input mutated: refs=%q audio=%v", refs, audio)
	}
}

func assertVideoInput(t *testing.T, got, want VideoInput) {
	t.Helper()
	if got.Prompt != want.Prompt || got.Duration != want.Duration || got.Resolution != want.Resolution ||
		got.AspectRatio != want.AspectRatio || got.FirstFrameAssetID != want.FirstFrameAssetID ||
		!slices.Equal(got.ReferenceAssetIDs, want.ReferenceAssetIDs) {
		t.Fatalf("used = %#v, want %#v", got, want)
	}
	if (got.Audio == nil) != (want.Audio == nil) || (got.Audio != nil && *got.Audio != *want.Audio) {
		t.Fatalf("audio = %v, want %v", got.Audio, want.Audio)
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

func videoErr(_ VideoInput, _ []string, err error) error { return err }
