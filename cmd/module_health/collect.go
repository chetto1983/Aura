package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func collect() []row {
	ents, _ := os.ReadDir("internal")
	var rows []row
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		r := row{name: e.Name()}
		walkMod(&r, filepath.Join("internal", e.Name()))
		rows = append(rows, r)
	}
	gods := loadBaseline()
	dupls := loadDupl()
	dates := gitDates()
	inbound := calcInbound(rows)
	for i := range rows {
		rows[i].gods = gods[rows[i].name]
		rows[i].dupls = dupls[rows[i].name]
		if d, ok := dates[rows[i].name]; ok {
			rows[i].lastMod = d
		} else {
			rows[i].lastMod = "–"
		}
		rows[i].inbound = inbound[rows[i].name]
	}
	return rows
}

func walkMod(r *row, dir string) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		prod := !strings.HasSuffix(path, "_test.go")
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close() //nolint:errcheck
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if prod {
				r.prodLOC++
				if strings.Contains(line, "TODO") || strings.Contains(line, "FIXME") {
					r.todos++
				}
			} else {
				r.testLOC++
			}
		}
		return nil
	})
}

func loadBaseline() map[string]int {
	m := map[string]int{}
	f, err := os.Open(baseFile)
	if err != nil {
		return m
	}
	defer f.Close() //nolint:errcheck
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		p := filepath.ToSlash(parts[1])
		segs := strings.Split(p, "/")
		if len(segs) >= 2 && segs[0] == "internal" {
			m[segs[1]]++
		}
	}
	return m
}

// fileToMod extracts the top-level module name from a dupl file reference
// (e.g. "D:\Aura\internal\agent\loop.go:36-39" → "agent").
func fileToMod(s string) string {
	if i := strings.LastIndex(s, ":"); i > 0 {
		rest := s[i+1:]
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			s = s[:i]
		}
	}
	s = filepath.ToSlash(s)
	i := strings.Index(s, "internal/")
	if i < 0 {
		return ""
	}
	parts := strings.SplitN(s[i+9:], "/", 2)
	return parts[0]
}

func loadDupl() map[string]int {
	m := map[string]int{}
	f, err := os.Open(duplFile)
	if err != nil {
		return m
	}
	defer f.Close() //nolint:errcheck
	type pair [2]string
	seen := map[pair]bool{}
	sc := bufio.NewScanner(f)
	const sep = ": duplicate of "
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, sep)
		if idx < 0 {
			continue
		}
		a := fileToMod(line[:idx])
		b := fileToMod(line[idx+len(sep):])
		if a == "" {
			continue
		}
		p := pair{a, b}
		if a > b {
			p = pair{b, a}
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		m[a]++
		if b != "" && b != a {
			m[b]++
		}
	}
	return m
}

func gitDates() map[string]string {
	out, err := exec.Command("git", "log", "--format=DATE %cs", "--name-only", "-n", "2000", "--", "internal/").Output()
	if err != nil {
		return nil
	}
	m := map[string]string{}
	date := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "DATE "):
			date = strings.TrimPrefix(line, "DATE ")
		case strings.HasPrefix(line, "internal/"):
			if segs := strings.SplitN(line[9:], "/", 2); len(segs) > 0 {
				if _, ok := m[segs[0]]; !ok {
					m[segs[0]] = date
				}
			}
		}
	}
	return m
}

func calcInbound(rows []row) map[string]int {
	m := map[string]int{}
	for _, r := range rows {
		m[r.name] = 0
	}
	for _, base := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			addImports(path, m)
			return nil
		})
	}
	return m
}

func addImports(path string, m map[string]int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	seen := map[string]bool{}
	const prefix = `"github.com/aura/aura/internal/`
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		idx := strings.Index(line, prefix)
		if idx < 0 {
			continue
		}
		after := line[idx+len(prefix):]
		end := strings.IndexByte(after, '"')
		if end < 0 {
			continue
		}
		mod := strings.SplitN(after[:end], "/", 2)[0]
		if !seen[mod] {
			seen[mod] = true
			m[mod]++
		}
	}
}
