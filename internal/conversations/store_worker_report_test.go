package conversations

import "testing"

func TestWorkerReportDeliveryKeyRequiresHostReceiptShape(t *testing.T) {
	for key, want := range map[string]bool{
		"":                    false,
		"ordinary-message":    false,
		"not-a-uuid:terminal": false,
		"21212121-2121-2121-2121-212121212121:terminal": true,
		"21212121-2121-2121-2121-212121212121:other":    false,
	} {
		if got := isWorkerReportDeliveryKey(key); got != want {
			t.Errorf("%q: got %v want %v", key, got, want)
		}
	}
}
