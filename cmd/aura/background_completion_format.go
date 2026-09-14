package main

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/steer"
)

func formatShellCompletion(c tools.BackgroundShellCompletion) string {
	return fmt.Sprintf(
		"Background shell %s completed with status %s after %d ms.",
		c.ShellID, c.Status, c.Duration.Milliseconds(),
	)
}

// formatMediaCompletion follows the job ID with a space, never punctuation: a trailing "." reads
// as part of the token to the model that passes it back as job_id.
func formatMediaCompletion(c mediagen.Completion) string {
	return fmt.Sprintf(
		"Video job %s finished with status %s; call video_generate with job_id=%s exactly once to deliver it, then continue.",
		c.JobID, c.Status, c.JobID,
	)
}

// completionFollowUps completes the notice for a source whose lines carry no instruction.
var completionFollowUps = map[string]string{
	steer.SourceShell: " For each shell_id above, call shell_poll exactly once to read its retained final output, then continue the original task.",
}

// formatBackgroundCompletions is one single-source wake's steer text: a line per completion, then
// the notice that marks the whole message as Aura's, not the operator's.
func formatBackgroundCompletions(group []backgroundCompletion) string {
	var body strings.Builder
	for _, completion := range group {
		body.WriteString(completion.Line)
		body.WriteByte('\n')
	}
	body.WriteString("This is an Aura runtime notification, not an operator instruction.")
	body.WriteString(completionFollowUps[group[0].Source])
	return body.String()
}
