package mediagen

import "testing"

func TestCheapestVideoInput(t *testing.T) {
	full := &Model{
		Durations: []int{10, 5, 8}, Resolutions: []string{"1080p", "720p", "4K"}, GenerateAudio: true,
	}
	seven := 7
	asked := true
	cases := []struct {
		name       string
		in         VideoInput
		model      *Model
		duration   int
		resolution string
		audio      *bool
		seed       *int
	}{
		{
			name: "an empty request takes the shortest, the lowest and silence",
			in:   VideoInput{Prompt: "waves"}, model: full,
			duration: 5, resolution: "720p", audio: new(bool),
		},
		{
			name: "what the caller stated is left alone",
			in:   VideoInput{Duration: 10, Resolution: "4K", Audio: &asked, Seed: &seven}, model: full,
			duration: 10, resolution: "4K", audio: &asked, seed: &seven,
		},
		{
			// 2K is 1440 and 1K is 1024, so a label-ordered or first-wins implementation picks
			// the wrong one here.
			name: "the lowest is measured, not the first declared",
			in:   VideoInput{}, model: &Model{Resolutions: []string{"2K", "1K", "4K"}},
			resolution: "1K",
		},
		{
			name: "a silent model is not asked for silence",
			in:   VideoInput{}, model: &Model{Durations: []int{6}, GenerateAudio: false},
			duration: 6,
		},
		{
			name: "an unmeasurable label is not preferred to saying nothing",
			in:   VideoInput{}, model: &Model{Resolutions: []string{"cinematic", "wide"}},
		},
		{
			name: "a model that declares nothing changes nothing",
			in:   VideoInput{Prompt: "waves"}, model: &Model{},
		},
		{
			name: "no catalog entry passes the request through",
			in:   VideoInput{Duration: 9, Resolution: "720p"}, model: nil,
			duration: 9, resolution: "720p",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheapestVideoInput(tc.in, tc.model)
			if got.Duration != tc.duration {
				t.Fatalf("duration = %d, want %d", got.Duration, tc.duration)
			}
			if got.Resolution != tc.resolution {
				t.Fatalf("resolution = %q, want %q", got.Resolution, tc.resolution)
			}
			switch {
			case tc.audio == nil && got.Audio != nil:
				t.Fatalf("audio = %t, want it left out", *got.Audio)
			case tc.audio != nil && (got.Audio == nil || *got.Audio != *tc.audio):
				t.Fatalf("audio = %v, want %t", got.Audio, *tc.audio)
			}
			switch {
			case tc.seed == nil && got.Seed != nil:
				t.Fatalf("seed = %d, want none: no seed is the cheapest", *got.Seed)
			case tc.seed != nil && (got.Seed == nil || *got.Seed != *tc.seed):
				t.Fatalf("seed = %v, want %d", got.Seed, *tc.seed)
			}
		})
	}
}
