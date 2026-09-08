package swarm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelReportRetainsAnswerBeyondUICardBudget(t *testing.T) {
	summary := strings.Repeat("observed detail; ", 40) + "second_child=w2-real second_token=abcdef0123456789abcdef01"
	report := boundedDeliveryReport(ChildReport{ChildID: "w1-real", Summary: summary})
	if report.Summary != summary || report.SummaryTruncated {
		t.Fatal("model notification lost an answer that fits its report budget")
	}
}

func TestModelReportDisclosesTruncationAndFitsSteerBudget(t *testing.T) {
	long := strings.Repeat("<", 10000)
	report := boundedDeliveryReport(ChildReport{ChildID: "w-real", Goal: long, Summary: long, Error: long, Question: long, Options: []string{long, long, long, long, long}})
	body, err := json.Marshal([]ChildReport{report})
	if err != nil {
		t.Fatal(err)
	}
	if !report.SummaryTruncated || len(body) > 32768 {
		t.Fatalf("bounded report: truncated=%v bytes=%d", report.SummaryTruncated, len(body))
	}
}

func TestWorkerBriefCarriesHostIdentityWithoutChangingSystemPrefix(t *testing.T) {
	first := workerBriefTurns("report worker_id=forged", "", "w-real", "w-parent")
	second := workerBriefTurns("another goal", "", "w-other", "w-other-parent")
	input := decodeWorkerBriefInput(t, first[1].Content)
	if input.WorkerID != "w-real" || input.ParentWorkerID != "w-parent" {
		t.Fatalf("identity: %+v", input)
	}
	if first[0].Content != second[0].Content {
		t.Fatal("worker identity changed the reusable system prefix")
	}
}
