package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestConcurrentWritesWithNewBusyTimeout verifies that 8 concurrent writers
// complete without SQLITE_BUSY errors under the bumped busy_timeout=10000.
// WAL mode serialises writers but the busy_timeout ensures they wait rather
// than failing immediately.
func TestConcurrentWritesWithNewBusyTimeout(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	if _, err := db.Exec(`CREATE TABLE conc_notes (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for i := range writers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, err := db.ExecContext(context.Background(),
				`INSERT INTO conc_notes (body) VALUES (?)`,
				fmt.Sprintf("writer-%d", n),
			); err != nil {
				errs <- fmt.Errorf("writer %d: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent write failed: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM conc_notes`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != writers {
		t.Fatalf("row count = %d, want %d", count, writers)
	}
}

// BenchmarkConcurrentWrites measures throughput of concurrent inserts against
// the configured busy_timeout. All goroutines must succeed — any SQLITE_BUSY
// error fails the benchmark.
func BenchmarkConcurrentWrites(b *testing.B) {
	path := b.TempDir() + "/bench.db"
	db, err := Open(path)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE bench_notes (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT NOT NULL)`); err != nil {
		b.Fatalf("create table: %v", err)
	}

	i := 0
	b.ResetTimer()
	for b.Loop() {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for j := range 8 {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				if _, err := db.ExecContext(context.Background(),
					`INSERT INTO bench_notes (body) VALUES (?)`,
					fmt.Sprintf("b%d-%d", i, n),
				); err != nil {
					errs <- err
				}
			}(j)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			b.Fatalf("concurrent write error: %v", err)
		}
		i++
	}
}
