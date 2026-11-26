package skills

import (
	"math"
	"regexp"
	"strconv"
)

// installer_ansi.go is the spike-011-proven ANSI strip + provenance parse for the
// `npx skills` transport output. The CLI emits ANSI color unconditionally (NO_COLOR
// is ignored, spike 011 / Pitfall 6), so every captured stdout/stderr stream is
// stripped BEFORE it is parsed or staged — an un-stripped escape sequence corrupts
// the parsed find lines and any captured body. The helpers here are PURE string
// transforms (no exec, no FS); the Installer (installer.go) owns the subprocess.

// ansiEscapeRe matches one CSI ANSI escape sequence `ESC [ <params> <final>` — the
// exact pattern spike 011 validated against the live `npx skills` output. It is the
// single ANSI chokepoint the transport routes every captured stream through.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// stripANSI removes every CSI escape sequence from s (spike 011). The find/add output
// is machine-shaped once stripped (fixed-line entries), so stripping is the only
// normalization the parse needs.
func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// catalogEntryRe matches one ANSI-stripped `npx skills find` result line:
// `owner[/repo]@skill N[K|M] installs` (spike 011, entryRE). It is machine output
// with a fixed line shape — never natural language, so a literal regex is correct
// here (unlike the prose-blocklist NFKC path). Group 1 is the source (owner or
// owner/repo), group 2 the skill name, groups 3 and 4 the install count's number and unit.
var catalogEntryRe = regexp.MustCompile(`^([A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)?)@([A-Za-z0-9._-]+) ([0-9]+(?:\.[0-9]+)?)([KM]?) installs$`)

// CatalogHit is one parsed `npx skills find` result: the install source the operator
// would pass to Install (owner/repo) plus the skill name and a human-readable install
// count token (e.g. "12K"). It is display + selection metadata only — no body.
type CatalogHit struct {
	Source   string `json:"source"`
	Skill    string `json:"skill"`
	Installs string `json:"installs"`
}

// parseCatalogHits strips ANSI from the raw `npx skills find` output and extracts every
// fixed-shape result line. A non-matching line (banner, usage text, table border) is
// skipped, so an empty-result search yields an empty slice (the empty-state edge).
func parseCatalogHits(raw string) []CatalogHit {
	var hits []CatalogHit
	for _, line := range splitLines(stripANSI(raw)) {
		m := catalogEntryRe.FindStringSubmatch(trimLine(line))
		if m == nil {
			continue
		}
		hits = append(hits, CatalogHit{Source: m[1], Skill: m[2], Installs: cliInstallCount(m[3], m[4])})
	}
	return hits
}

// cliInstallCount reads the CLI's count token (226.3K) as the count it stands for and formats
// it as the JSON catalog path does (226K, compactInstallCount), so one count reads the same
// whichever path served it.
func cliInstallCount(number, unit string) string {
	value, _ := strconv.ParseFloat(number, 64) // catalogEntryRe admits only digits and one fraction
	switch unit {
	case "K":
		value *= 1_000
	case "M":
		value *= 1_000_000
	}
	return compactInstallCount(int64(math.Round(value)))
}
