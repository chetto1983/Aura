package skills

import (
	"testing"
)

// BenchmarkLoaderLoadAllCached measures the steady-state cost of LoadAll
// when the result is served from the slice 11m TTL cache. This is what
// every Telegram turn pays under normal use (no admin install/delete).
func BenchmarkLoaderLoadAllCached(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 20; i++ {
		writeBenchSkill(b, dir, i)
	}
	loader := NewLoader(dir)
	if _, err := loader.LoadAll(); err != nil { // warm cache
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loader.LoadAll(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoaderLoadAllUncached measures the cost of the underlying
// directory walk + YAML parse path. Pre-slice-11m, every Telegram turn
// paid this. The delta vs BenchmarkLoaderLoadAllCached is the per-turn
// disk I/O we eliminated.
func BenchmarkLoaderLoadAllUncached(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 20; i++ {
		writeBenchSkill(b, dir, i)
	}
	loader := NewLoader(dir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loader.loadAllUncached(); err != nil {
			b.Fatal(err)
		}
	}
}

func writeBenchSkill(b *testing.B, dir string, i int) {
	b.Helper()
	name := "skill_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	writeSkill(b, dir, name, name, "bench skill "+name, "body for "+name)
}
