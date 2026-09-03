package llm

import "testing"

// The cap a model change hands forward. A provider that publishes no completion limit
// must not leave the previous provider's number in place: that number describes another
// model and is about to be read against another window. Config.Validate refuses a
// non-positive cap, so the reset is to what the NEW window affords, not to zero.
func TestDerivedMaxOutputTokensFollowsTheNewWindow(t *testing.T) {
	for window, want := range map[int]int{
		262144:  defaultMaxOutputTokens,             // room to spare: the default stands
		1000000: defaultMaxOutputTokens,             // and on a huge window too
		32768:   32768 * outputReservePercent / 100, // too small for it: a share instead
		0:       defaultMaxOutputTokens,             // unknown window: the default
	} {
		if got := DerivedMaxOutputTokens(window); got != want {
			t.Errorf("DerivedMaxOutputTokens(%d) = %d, want %d", window, got, want)
		}
	}
	// The property that matters: whatever the window, the budget it leaves is positive.
	for _, window := range []int{4096, 32768, 131072, 262144, 1000000} {
		cap := DerivedMaxOutputTokens(window)
		if budget := window - OutputReserve(window, cap) - PromptHeadroom(window); budget <= 0 {
			t.Errorf("window %d leaves prompt budget %d", window, budget)
		}
	}
}
