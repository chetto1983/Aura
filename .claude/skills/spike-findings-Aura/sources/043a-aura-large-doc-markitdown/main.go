package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	asyncTierMinBytes  = 5 << 20
	refuseTierMinBytes = 50 << 20
)

type observation struct {
	Name   string
	Pass   bool
	Detail string
}

func main() {
	fmt.Println("[SPIKE] 043a Aura large-document markitdown path")

	root := auraRoot()
	var observations []observation
	var guards int

	documentsPath := filepath.Join(root, "internal", "channels", "telegram", "documents.go")
	appPath := filepath.Join(root, "docker", "markitdown", "app.py")
	testsPath := filepath.Join(root, "internal", "channels", "telegram", "documents_test.go")

	documentsGo, err := os.ReadFile(documentsPath)
	if err != nil {
		failGuard(&observations, &guards, "read Aura documents.go", err.Error())
	} else {
		text := string(documentsGo)
		add(&observations, strings.Contains(text, "case len(payload) >= refuseTierMinBytes"), "refuse boundary uses >=", "documents.go refuses exactly 50 MiB with current switch")
		add(&observations, strings.Contains(text, "case len(payload) >= asyncTierMinBytes"), "async boundary uses >=", "documents.go routes exactly 5 MiB to async with current switch")
		add(&observations, strings.Contains(text, "bytes.Buffer") && strings.Contains(text, "multipart.NewWriter(&body)"), "multipart request buffered in memory", "postConvert builds the whole multipart body before HTTP POST")
	}

	appPy, err := os.ReadFile(appPath)
	if err != nil {
		failGuard(&observations, &guards, "read markitdown app.py", err.Error())
	} else {
		add(&observations, strings.Contains(string(appPy), "data = await file.read()"), "sidecar reads entire upload", "FastAPI wrapper materializes the whole uploaded file before temp-file conversion")
	}

	tests, err := os.ReadFile(testsPath)
	if err != nil {
		failGuard(&observations, &guards, "read documents tests", err.Error())
	} else {
		text := string(tests)
		exact5 := strings.Contains(text, "asyncTierMinBytes)") || strings.Contains(text, "asyncTierMinBytes,")
		exact50 := strings.Contains(text, "refuseTierMinBytes)") || strings.Contains(text, "refuseTierMinBytes,")
		exactTestsAbsent := !exact5 && !exact50
		add(&observations, !exactTestsAbsent, "exact boundary tests absent", "tests cover +1 around thresholds but not exact 5 MiB or exact 50 MiB")
	}

	fmt.Println("[BOUNDARIES]")
	for _, size := range []int{
		asyncTierMinBytes - 1,
		asyncTierMinBytes,
		asyncTierMinBytes + 1,
		refuseTierMinBytes - 1,
		refuseTierMinBytes,
		refuseTierMinBytes + 1,
	} {
		actual := auraTier(size)
		intended := intendedTier(size)
		status := "ok"
		if actual != intended {
			status = "mismatch"
			add(&observations, false, fmt.Sprintf("boundary mismatch at %s", mib(size)), fmt.Sprintf("actual=%s intended=%s", actual, intended))
		}
		fmt.Printf("  size=%-12s actual=%-7s intended=%-7s %s\n", mib(size), actual, intended, status)
	}

	fmt.Println("[MULTIPART]")
	for _, size := range []int{asyncTierMinBytes + 1, refuseTierMinBytes - 1} {
		result, err := measureMultipart(size)
		if err != nil {
			failGuard(&observations, &guards, "multipart post probe", err.Error())
			continue
		}
		residentEstimate := size + result.bodyLen
		ratio := float64(residentEstimate) / float64(size)
		fmt.Printf("  payload=%-12s multipart=%-12s total_alloc=%-12s resident_estimate=%-12s resident_ratio=%.2fx\n", mib(size), mib(result.bodyLen), mib64(result.totalAlloc), mib(residentEstimate), ratio)
		amplified := ratio > 1.70
		add(&observations, !amplified, fmt.Sprintf("memory amplification observed for %s", mib(size)), "payload plus multipart body are both resident before the sidecar also reads the upload")
	}

	fmt.Println("[OBSERVATIONS]")
	for _, obs := range observations {
		label := "PASS"
		if !obs.Pass {
			label = "FINDING"
		}
		fmt.Printf("  [%s] %s: %s\n", label, obs.Name, obs.Detail)
	}

	fmt.Printf("[SUMMARY] verdict=PARTIAL guard_failures=%d finding_count=%d\n", guards, countFindings(observations))
	if guards > 0 {
		os.Exit(1)
	}
}

func auraRoot() string {
	if env := os.Getenv("AURA_ROOT"); env != "" {
		return env
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func auraTier(size int) string {
	switch {
	case size >= refuseTierMinBytes:
		return "refuse"
	case size >= asyncTierMinBytes:
		return "async"
	default:
		return "sync"
	}
}

func intendedTier(size int) string {
	switch {
	case size > refuseTierMinBytes:
		return "refuse"
	case size <= asyncTierMinBytes:
		return "sync"
	default:
		return "async"
	}
}

type multipartResult struct {
	bodyLen    int
	totalAlloc uint64
}

func measureMultipart(size int) (multipartResult, error) {
	payload := bytes.Repeat([]byte("a"), size)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"drained": n})
	}))
	defer server.Close()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "large.md")
	if err != nil {
		return multipartResult{}, err
	}
	if _, err := io.Copy(part, bytes.NewReader(payload)); err != nil {
		return multipartResult{}, err
	}
	if err := mw.Close(); err != nil {
		return multipartResult{}, err
	}
	bodyLen := body.Len()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/convert", &body)
	if err != nil {
		return multipartResult{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return multipartResult{}, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return multipartResult{}, fmt.Errorf("sidecar probe returned status %s", resp.Status)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return multipartResult{bodyLen: bodyLen, totalAlloc: after.TotalAlloc - before.TotalAlloc}, nil
}

func add(observations *[]observation, pass bool, name string, detail string) {
	*observations = append(*observations, observation{Name: name, Pass: pass, Detail: detail})
}

func failGuard(observations *[]observation, guards *int, name string, detail string) {
	*guards++
	add(observations, false, name, detail)
}

func countFindings(observations []observation) int {
	var count int
	for _, obs := range observations {
		if !obs.Pass {
			count++
		}
	}
	return count
}

func mib(size int) string {
	return fmt.Sprintf("%.2fMiB", float64(size)/(1<<20))
}

func mib64(size uint64) string {
	return fmt.Sprintf("%.2fMiB", float64(size)/(1<<20))
}
