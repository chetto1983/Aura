package tools

import "strings"

// shell_exec cwd-marker tracking (split out of shell_exec.go on touch, 600-LOC cap): the shell's
// final $PWD is fenced on the last stdout line so Execute can carry a `cd` across calls (Claude-
// Code Bash-tool parity). extractCwdMarker strips it before the model sees output. Both the host
// POSIX shell and the routed box shell (/bin/sh) use this — the box variant uses PLAIN pwd
// (never `pwd -W`, which is a Git-Bash/Windows-only builtin — 37-RESEARCH Pitfall 6).

// cwdMarker fences the shell's final $PWD on the last stdout line.
const cwdMarker = "__AURA_CWD__"

// wrapForCwdTracking groups the user command, preserves its exit code, and prints the final
// working directory behind the marker. `pwd -W` (Git Bash) yields a Windows-valid path so the
// next call's cmd.Dir works; plain pwd is the POSIX fallback. HOST path only.
func wrapForCwdTracking(command string) string {
	return "{\n" + command + "\n}\n__aura_ec=$?\nprintf '\\n%s %s\\n' '" + cwdMarker + "' \"$(pwd -W 2>/dev/null || pwd)\"\nexit $__aura_ec"
}

// wrapForCwdTrackingBox is the ROUTED (in-box) cwd-tracking wrap: identical to wrapForCwdTracking
// but with PLAIN `pwd` (the box is a POSIX Linux /bin/sh where `pwd -W` does not exist — Pitfall 6).
func wrapForCwdTrackingBox(command string) string {
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

func removeCwdMarkerLine(output string) (clean, dir string) {
	idx := strings.LastIndex(output, cwdMarker+" ")
	if idx < 0 {
		return output, ""
	}
	lineStart := idx
	for lineStart > 0 && output[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd := idx + len(cwdMarker) + 1
	for lineEnd < len(output) && output[lineEnd] != '\n' {
		lineEnd++
	}
	dir = strings.TrimSpace(output[idx+len(cwdMarker)+1 : lineEnd])
	if lineEnd < len(output) && output[lineEnd] == '\n' {
		lineEnd++
	}
	clean = output[:lineStart] + output[lineEnd:]
	clean = strings.TrimSuffix(clean, "\n")
	return clean, dir
}
