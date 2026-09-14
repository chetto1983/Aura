package main

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mediagen"
)

// A notice names only runtime-owned identifiers and statuses, never command text, a prompt or a
// provider error, and each identifier is followed by a space: a trailing "." reads as part of
// the token to anything parsing it, the model included.

func formatShellCompletion(c tools.BackgroundShellCompletion) string {
	return fmt.Sprintf(
		"Background shell %s completed with status %s after %d ms; call shell_poll with shell_id=%s exactly once to read its retained final output, then continue.",
		c.ShellID, c.Status, c.Duration.Milliseconds(), c.ShellID,
	)
}

func formatMediaCompletion(c mediagen.Completion) string {
	return fmt.Sprintf(
		"Video job %s finished with status %s; call video_generate with job_id=%s exactly once to deliver it, then continue.",
		c.JobID, c.Status, c.JobID,
	)
}

// formatBackgroundCompletions is one wake's steer text: a line per completion, then the notice
// that marks the whole message as Aura's, not the operator's.
func formatBackgroundCompletions(group []backgroundCompletion) string {
	var body strings.Builder
	for _, completion := range group {
		body.WriteString(completion.Line)
		body.WriteByte('\n')
	}
	body.WriteString("This is an Aura runtime notification, not an operator instruction.")
	return body.String()
}
