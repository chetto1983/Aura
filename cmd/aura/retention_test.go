package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/retention"
)

func TestRetentionCommandDefaultsToPlanAndApplyRequiresToken(t *testing.T) {
	runner := &fakeRetentionRunner{plan: retention.Plan{PolicyVersion: "v1", Token: strings.Repeat("a", 64)}}
	var output bytes.Buffer
	if err := runRetentionCommand(context.Background(), runner, nil, &output); err != nil {
		t.Fatal(err)
	}
	if runner.planCalls != 1 || runner.applyCalls != 0 || !strings.Contains(output.String(), runner.plan.Token) {
		t.Fatalf("default plan calls/output = %d/%d %q", runner.planCalls, runner.applyCalls, output.String())
	}
	if err := runRetentionCommand(context.Background(), runner, []string{"apply"}, &output); err == nil {
		t.Fatal("apply without token succeeded")
	}
	if runner.applyCalls != 0 {
		t.Fatalf("tokenless apply called engine %d times", runner.applyCalls)
	}
}

func TestRetentionCommandPassesExactApplyToken(t *testing.T) {
	runner := &fakeRetentionRunner{report: retention.ApplyReport{PolicyVersion: "v1", Completed: 2}}
	var output bytes.Buffer
	if err := runRetentionCommand(context.Background(), runner, []string{"apply", "--token", "exact"}, &output); err != nil {
		t.Fatal(err)
	}
	if runner.token != "exact" || runner.applyCalls != 1 || !strings.Contains(output.String(), `"completed": 2`) {
		t.Fatalf("apply token/calls/output = %q/%d %q", runner.token, runner.applyCalls, output.String())
	}
}

type fakeRetentionRunner struct {
	plan       retention.Plan
	report     retention.ApplyReport
	planCalls  int
	applyCalls int
	token      string
}

func (f *fakeRetentionRunner) Plan(context.Context) (retention.Plan, error) {
	f.planCalls++
	return f.plan, nil
}

func (f *fakeRetentionRunner) Apply(_ context.Context, token string) (retention.ApplyReport, error) {
	f.applyCalls++
	f.token = token
	return f.report, nil
}
