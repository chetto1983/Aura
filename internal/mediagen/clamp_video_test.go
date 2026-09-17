package mediagen

import (
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

// TestClampVideoLastFrame pins the end frame against the frame set the model declares. A model
// that starts from an image need not also end on one, so "supports frames" is not the question:
// the clamp asks for last_frame by name. The refusal is total rather than a dropped field,
// because a clip that ignores the end frame is billed just the same.
func TestClampVideoLastFrame(t *testing.T) {
	bothFrames := &Model{ID: "google/veo-3.1-lite", FrameImages: []string{"first_frame", "last_frame"}}
	firstOnly := &Model{ID: "start-only", FrameImages: []string{"first_frame"}}
	in := VideoInput{Prompt: "p", FirstFrameAssetID: "start", LastFrameAssetID: "end"}

	used, notes, err := ClampVideo(in, firstOnly)
	if ErrorCode(err) != "unsupported" || notes != nil || used.Prompt != "" {
		t.Fatalf("err = %v, used = %#v, notes = %q; want an unsupported refusal", err, used, notes)
	}
	for _, part := range []string{"cannot end on a given image", "Nothing was generated", "Remove the end frame"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("message %q lacks %q", err.Error(), part)
		}
	}

	used, notes, err = ClampVideo(in, bothFrames)
	if err != nil {
		t.Fatal(err)
	}
	assertVideoInput(t, used, in)
	if len(notes) != 0 {
		t.Fatalf("notes = %q, want none", notes)
	}

	used, _, err = ClampVideo(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertVideoInput(t, used, in)
}

// TestClampVideoSeed pins the seed against Model.Seed. A model that ignores an unknown field is
// not a reason to send one: a seed that was accepted but not honoured would promise a
// reproducibility the clip does not have, so it is dropped and the caller is told.
func TestClampVideoSeed(t *testing.T) {
	seed := 7
	in := VideoInput{Prompt: "p", Seed: &seed}

	used, notes, err := ClampVideo(in, &Model{ID: "no-seed", Seed: false})
	if err != nil {
		t.Fatal(err)
	}
	assertVideoInput(t, used, VideoInput{Prompt: "p"})
	if !slices.Equal(notes, []string{"seed is not supported by this model; omitted"}) {
		t.Fatalf("notes = %q, want the dropped-seed note", notes)
	}

	used, notes, err = ClampVideo(in, &Model{ID: "seeded", Seed: true})
	if err != nil {
		t.Fatal(err)
	}
	assertVideoInput(t, used, in)
	if len(notes) != 0 {
		t.Fatalf("notes = %q, want none", notes)
	}

	used, _, err = ClampVideo(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertVideoInput(t, used, in)
	if seed != 7 {
		t.Fatalf("caller seed mutated to %d", seed)
	}
}

func assertVideoInput(t *testing.T, got, want VideoInput) {
	t.Helper()
	if got.Prompt != want.Prompt || got.Duration != want.Duration || got.Resolution != want.Resolution ||
		got.AspectRatio != want.AspectRatio || got.FirstFrameAssetID != want.FirstFrameAssetID ||
		got.LastFrameAssetID != want.LastFrameAssetID ||
		!slices.Equal(got.ReferenceAssetIDs, want.ReferenceAssetIDs) {
		t.Fatalf("used = %#v, want %#v", got, want)
	}
	if (got.Audio == nil) != (want.Audio == nil) || (got.Audio != nil && *got.Audio != *want.Audio) {
		t.Fatalf("audio = %v, want %v", got.Audio, want.Audio)
	}
	if (got.Seed == nil) != (want.Seed == nil) || (got.Seed != nil && *got.Seed != *want.Seed) {
		t.Fatalf("seed = %v, want %v", got.Seed, want.Seed)
	}
}

func videoErr(_ VideoInput, _ []string, err error) error { return err }
