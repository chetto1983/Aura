package swarm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

func TestReportMarkerFollowsDurableConversationWrite(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "recorded", true: "record failed"}[fails], func(t *testing.T) {
			store, recorder := &fakeDelegationStore{}, &fakeConversationRecorder{}
			if fails {
				recorder.err = errors.New("record unavailable")
			}
			l := &DelegationClaimLoop{Store: store, Delivery: &DelegationDelivery{Recorder: recorder, Archiver: successfulReportArchiver()}}
			l.Worker.Cfg.RunDir = t.TempDir()
			job := documents.IngestionJob{ID: "j1", IdentityID: "identity-1"}
			payload := DelegationPayload{Goal: "g", ConversationID: "conv", ChildID: "w1", FanoutKey: "f-test"}
			_ = l.deliverSuccess(context.Background(), job, payload, ChildReport{ChildID: "w1", Status: StatusOK, Summary: "done"})
			path := filepath.Join(l.Worker.Cfg.RunDir, "conv", "swarm", "w1.jsonl")
			data, err := os.ReadFile(path)
			if fails {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("uncommitted report was announced: %s, %v", data, err)
				}
			} else if err != nil || !strings.Contains(string(data), `"swarm_report_recorded":true`) {
				t.Fatalf("committed report has no refresh signal: %s, %v", data, err)
			}
		})
	}
}
