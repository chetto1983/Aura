package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// manualTiers is manualTiersPath: what CI does not run, and why. Every entry carries a
// reason and is checked for staleness by tierViolations.
//
//	[tags]         <tag> | <reason>            every file behind the tag runs by hand
//	[files]        <path> | <reason>           one tagged test file no invocation reaches
//	[routes]       <tag> | <via> | <reason>    a route the text credits but control flow never reaches
//	[conditional]  <tag> | <reason>            a step that may not run on a push still counts for the tag
type manualTiers struct {
	tags        map[string]string
	files       map[string]string
	routes      map[routeKey]string
	conditional map[string]string
}

type routeKey struct{ tag, via string }

var manualSectionFields = map[string]int{"tags": 2, "files": 2, "routes": 3, "conditional": 2}

func readManualTiers(file string) (manualTiers, error) {
	manual := manualTiers{tags: map[string]string{}, files: map[string]string{}, routes: map[routeKey]string{}, conditional: map[string]string{}}
	data, err := os.ReadFile(file)
	if err != nil {
		return manual, err
	}
	section := ""
	seen := map[string]bool{}
	for n, line := range slices.Collect(strings.Lines(string(data))) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, ok := strings.CutPrefix(line, "["); ok {
			section = strings.TrimSuffix(name, "]")
			if manualSectionFields[section] == 0 {
				return manual, fmt.Errorf("%s:%d: unknown section [%s]", file, n+1, section)
			}
			continue
		}
		if section == "" {
			return manual, fmt.Errorf("%s:%d: entry before any [section]", file, n+1)
		}
		fields := strings.SplitN(line, "|", manualSectionFields[section])
		for len(fields) < manualSectionFields[section] {
			fields = append(fields, "")
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		key := section + "|" + strings.Join(fields[:len(fields)-1], "|")
		if seen[key] {
			return manual, fmt.Errorf("%s:%d: %s is listed twice in [%s]", file, n+1, fields[0], section)
		}
		seen[key] = true
		reason := fields[len(fields)-1]
		switch section {
		case "tags":
			manual.tags[fields[0]] = reason
		case "files":
			manual.files[fields[0]] = reason
		case "routes":
			manual.routes[routeKey{fields[0], fields[1]}] = reason
		case "conditional":
			manual.conditional[fields[0]] = reason
		}
	}
	return manual, nil
}

// credited applies the list to what the walk found: a listed route loses its tag, and a
// step that may not run on a push keeps only the tags listed as conditional.
func (m manualTiers) credited(runs []goTestRun) []goTestRun {
	out := make([]goTestRun, 0, len(runs))
	for _, run := range runs {
		run.tags = slices.DeleteFunc(slices.Clone(run.tags), func(tag string) bool {
			if _, never := m.routes[routeKey{tag, run.via}]; never {
				return true
			}
			_, accepted := m.conditional[tag]
			return run.condition != "" && !accepted
		})
		out = append(out, run)
	}
	return out
}

// staleness reports list entries with no reason or nothing left to excuse. excused holds
// the [tags] entries that excuse at least one unreached file.
func (m manualTiers) staleness(files map[string]tierTestFile, runs []goTestRun, excused map[string]bool) []string {
	var out []string
	empty := func(section, key, reason string) {
		if reason == "" {
			out = append(out, fmt.Sprintf("%s [%s] %s has no reason", manualTiersPath, section, key))
		}
	}
	credits := func(tag string, match func(goTestRun) bool) bool {
		return slices.ContainsFunc(runs, func(r goTestRun) bool { return match(r) && slices.Contains(r.tags, tag) })
	}
	for _, tag := range slices.Sorted(maps.Keys(m.tags)) {
		empty("tags", tag, m.tags[tag])
		if !excused[tag] {
			out = append(out, fmt.Sprintf("%s [tags] %s excuses no unreached test file: remove it", manualTiersPath, tag))
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(m.files)) {
		empty("files", rel, m.files[rel])
		if f, ok := files[rel]; !ok || len(f.gating) == 0 {
			out = append(out, fmt.Sprintf("%s [files] %s is not a tracked test file behind a feature tag: remove it", manualTiersPath, rel))
		}
	}
	for _, key := range slices.SortedFunc(maps.Keys(m.routes), func(a, b routeKey) int {
		return strings.Compare(a.tag+" "+a.via, b.tag+" "+b.via)
	}) {
		empty("routes", key.tag, m.routes[key])
		if !credits(key.tag, func(r goTestRun) bool { return r.via == key.via }) {
			out = append(out, fmt.Sprintf("%s [routes] %s via %s: that route no longer credits the tag: remove it", manualTiersPath, key.tag, key.via))
		}
	}
	for _, tag := range slices.Sorted(maps.Keys(m.conditional)) {
		empty("conditional", tag, m.conditional[tag])
		if !credits(tag, func(r goTestRun) bool { return r.condition != "" }) {
			out = append(out, fmt.Sprintf("%s [conditional] %s: no conditional step passes it any more: remove it", manualTiersPath, tag))
		}
	}
	return out
}
