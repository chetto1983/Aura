package main

// Process exit codes shared across aura subcommands (sysexits.h conventions).
// A normal run exits 0; these classify failures so scripts/CI can branch on them.
const (
	exitUnreachable = 70 // a required service (e.g. SearXNG) is unreachable
	exitInfra       = 71 // other infrastructure fault (malformed protocol, etc.)
	exitUsage       = 64 // EX_USAGE — bad arguments / invocation
)
