package tools

import "strings"

// shell_exec cwd-marker tracking (split out of shell_exec.go on touch, 600-LOC cap): the box
// shell's final $PWD is fenced on the last stdout line so Execute can carry a `cd` across calls
// (Claude-Code Bash-tool parity). extractCwdMarker strips it before the model sees output.

// cwdMarker fences the shell's final $PWD on the last stdout line.
const cwdMarker = "__AURA_CWD__"

// wrapForCwdTracking groups the user command, preserves its exit code, and prints the final
// working directory behind the marker. PLAIN `pwd`: the box is a POSIX Linux /bin/sh, where the
// Git-Bash-only `pwd -W` the deleted host path needed does not exist (37-RESEARCH Pitfall 6).
func wrapForCwdTracking(command string) string {
	return "{\n" + command + "\n}\n__aura_ec=$?\nprintf '\\n%s %s\\n' '" + cwdMarker + "' \"$(pwd)\"\nexit $__aura_ec"
}

// extractCwdMarker splits the marker line back out of stdout: returns the cleaned output (the
// marker's injected leading newline removed) and the captured dir ("" when the marker never
// printed).
func extractCwdMarker(stdout string) (clean, dir string) {
	idx := strings.LastIndex(stdout, cwdMarker+" ")
	if idx < 0 {
		return stdout, ""
	}
	tail := stdout[idx+len(cwdMarker)+1:]
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
		tail = tail[:nl]
	}
	clean = stdout[:idx]
	clean = strings.TrimSuffix(clean, "\n") // the printf-injected separator, not user output
	return clean, strings.TrimSpace(tail)
}
