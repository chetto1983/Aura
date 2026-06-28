package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	asyncTierMinBytes  = 5 << 20
	refuseTierMinBytes = 50 << 20
)

type status string

const (
	statusAccepted status = "accepted"
	statusRunning  status = "running"
	statusSuccess  status = "success"
	statusFailure  status = "failure"
	statusRefused  status = "refused"
	statusCanceled status = "canceled"
)

type job struct {
	ID       string
	FileName string
	Size     int
	Status   status
	Progress int
	Error    string
	Messages []string
}

type manager struct {
	next int
	jobs map[string]*job
}

type check struct {
	Name   string
	Pass   bool
	Detail string
}

func main() {
	fmt.Println("[SPIKE] 046 Telegram ingest job UX contract")

	m := &manager{jobs: map[string]*job{}}
	var checks []check

	success := m.Submit("audit-large.pdf", 25<<20)
	checks = append(checks, check{"accepted before work", success.Status == statusAccepted && success.ID != "", fmt.Sprintf("id=%s status=%s", success.ID, success.Status)})
	m.Progress(success.ID, 10, "converting")
	m.Progress(success.ID, 55, "chunking")
	m.Succeed(success.ID, "stored 194 chunks")
	checks = append(checks, check{"success lifecycle", hasStatuses(success, statusAccepted, statusRunning, statusSuccess) && success.Progress == 100, strings.Join(success.Messages, " | ")})

	failure := m.Submit("broken.docx", 12<<20)
	m.Progress(failure.ID, 15, "converting")
	m.Fail(failure.ID, "markitdown returned 422")
	checks = append(checks, check{"failure visible", failure.Status == statusFailure && strings.Contains(last(failure.Messages), "failed"), strings.Join(failure.Messages, " | ")})

	refused := m.Submit("too-big.pdf", refuseTierMinBytes+1)
	checks = append(checks, check{"oversize refusal", refused.Status == statusRefused && refused.ID == "", strings.Join(refused.Messages, " | ")})

	cancel := m.Submit("cancel-me.pdf", 9<<20)
	m.Progress(cancel.ID, 20, "embedding")
	m.Cancel(cancel.ID)
	checks = append(checks, check{"cancel running job", cancel.Status == statusCanceled && strings.Contains(last(cancel.Messages), "canceled"), strings.Join(cancel.Messages, " | ")})

	polled, ok := m.Status(success.ID)
	checks = append(checks, check{"pollable status", ok && polled.Status == statusSuccess && polled.Progress == 100, fmt.Sprintf("id=%s status=%s progress=%d", polled.ID, polled.Status, polled.Progress)})
	_, missing := m.Status("job-missing")
	checks = append(checks, check{"unknown status explicit", !missing, "missing job returns ok=false"})

	verdict := "VALIDATED"
	for _, c := range checks {
		if !c.Pass {
			verdict = "INVALIDATED"
		}
	}

	fmt.Println("[JOBS]")
	for _, id := range m.IDs() {
		j := m.jobs[id]
		fmt.Printf("  %s file=%s status=%s progress=%d messages=%d\n", j.ID, j.FileName, j.Status, j.Progress, len(j.Messages))
	}

	fmt.Println("[CHECKS]")
	for _, c := range checks {
		label := "PASS"
		if !c.Pass {
			label = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s\n", label, c.Name, c.Detail)
	}

	fmt.Printf("[SUMMARY] verdict=%s jobs=%d\n", verdict, len(m.jobs))
	if verdict != "VALIDATED" {
		os.Exit(1)
	}
}

func (m *manager) Submit(fileName string, size int) *job {
	if size > refuseTierMinBytes {
		return &job{FileName: fileName, Size: size, Status: statusRefused, Messages: []string{"refused: file is over 50 MiB"}}
	}
	if size <= asyncTierMinBytes {
		return &job{FileName: fileName, Size: size, Status: statusSuccess, Progress: 100, Messages: []string{"converted synchronously"}}
	}
	m.next++
	j := &job{
		ID:       fmt.Sprintf("ingest-%04d", m.next),
		FileName: fileName,
		Size:     size,
		Status:   statusAccepted,
		Progress: 0,
		Messages: []string{"accepted: " + fileName},
	}
	m.jobs[j.ID] = j
	return j
}

func (m *manager) Progress(id string, progress int, label string) {
	j, ok := m.jobs[id]
	if !ok || terminal(j.Status) {
		return
	}
	j.Status = statusRunning
	j.Progress = progress
	j.Messages = append(j.Messages, fmt.Sprintf("progress %d%%: %s", progress, label))
}

func (m *manager) Succeed(id string, detail string) {
	j, ok := m.jobs[id]
	if !ok || terminal(j.Status) {
		return
	}
	j.Status = statusSuccess
	j.Progress = 100
	j.Messages = append(j.Messages, "success: "+detail)
}

func (m *manager) Fail(id string, err string) {
	j, ok := m.jobs[id]
	if !ok || terminal(j.Status) {
		return
	}
	j.Status = statusFailure
	j.Error = err
	j.Messages = append(j.Messages, "failed: "+err)
}

func (m *manager) Cancel(id string) {
	j, ok := m.jobs[id]
	if !ok || terminal(j.Status) {
		return
	}
	j.Status = statusCanceled
	j.Messages = append(j.Messages, "canceled")
}

func (m *manager) Status(id string) (*job, bool) {
	j, ok := m.jobs[id]
	return j, ok
}

func (m *manager) IDs() []string {
	ids := make([]string, 0, len(m.jobs))
	for id := range m.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func terminal(s status) bool {
	return s == statusSuccess || s == statusFailure || s == statusRefused || s == statusCanceled
}

func hasStatuses(j *job, statuses ...status) bool {
	text := strings.Join(j.Messages, "\n")
	for _, s := range statuses {
		if s == statusAccepted && !strings.Contains(text, "accepted") {
			return false
		}
		if s == statusRunning && !strings.Contains(text, "progress") {
			return false
		}
		if s == statusSuccess && !strings.Contains(text, "success") {
			return false
		}
	}
	return true
}

func last(in []string) string {
	if len(in) == 0 {
		return ""
	}
	return in[len(in)-1]
}
