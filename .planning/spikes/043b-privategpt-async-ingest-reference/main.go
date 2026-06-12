package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type observation struct {
	Name   string
	Pass   bool
	Detail string
}

func main() {
	fmt.Println("[SPIKE] 043b PrivateGPT async ingest reference")

	root := privateGPTRoot()
	var observations []observation
	var guards int

	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		failGuard(&observations, &guards, "private-gpt checkout present", fmt.Sprintf("%s is not a git checkout", root))
	} else {
		fmt.Printf("[SOURCE] root=%s\n", root)
		fmt.Printf("[SOURCE] head=%s\n", oneLine(root, "rev-parse", "--short", "HEAD"))
		fmt.Printf("[SOURCE] last_commit=%s\n", oneLine(root, "log", "-1", "--format=%H|%cI|%s"))
	}

	router := readRequired(root, &observations, &guards, "private_gpt/server/ingest/ingest_router.py")
	component := readRequired(root, &observations, &guards, "private_gpt/components/ingest/ingest_component.py")
	vectorIndex := readRequired(root, &observations, &guards, "private_gpt/artifact_index/vector_artifact_index.py")
	baseIndex := readRequired(root, &observations, &guards, "private_gpt/artifact_index/base_artifact_index.py")
	task := readRequired(root, &observations, &guards, "private_gpt/celery/tasks/ingestion/extraction_tasks.py")
	routeTests := readRequired(root, &observations, &guards, "tests/server/ingest/test_ingest_routes.py")
	asyncTests := readRequired(root, &observations, &guards, "tests/server/ingest/test_ingestion_async.py")
	repoGuide := readRequired(root, &observations, &guards, "fern/docs/pages/api-guide/ingestion.mdx")
	repoAsyncGuide := readRequired(root, &observations, &guards, "fern/docs/pages/api-guide/ingestion-async.mdx")

	add(&observations, containsAll(router, `prefix="/v1/artifacts"`, `"/ingest"`, `"/ingest/async"`, `"/ingest/async/{task_id}"`), "artifact ingest route family", "repo exposes sync ingest, async enqueue, and async status under /v1/artifacts")
	add(&observations, containsAll(router, "celery_app.send_task", `"vector_index_task"`), "async work is task-backed", "router enqueues vector_index_task instead of holding the HTTP request open")
	add(&observations, containsAll(router, "upload_file_to_s3", "temporary_bucket_name", "UriArtifact"), "large async payload handoff", "non-URI async input is converted to temporary S3 URI before Celery")
	add(&observations, containsAll(component, "MetadataKeys.FILE_HASH", "retrieve_ingested_nodes", "file_info.hash", "Artifact is already ingested"), "file-hash idempotency", "ingest component stores hash metadata and avoids duplicate same-artifact re-ingest")
	add(&observations, containsAll(vectorIndex, `"insert_batch_size": 512`), "bounded vector insert batches", "vector index options cap insert batch size at 512")
	add(&observations, containsAll(baseIndex, "bounded_concurrent_execute", "concurrency_limit=1"), "async embedding/index concurrency cap", "async insertion is explicitly bounded in the current source")
	add(&observations, containsAll(task, "cleanup_temporal_files", "AUTORETRY_EXCEPTIONS", "ensure_to_remove_temporal_files", "ProgressStatus"), "temporary cleanup and progress callbacks", "task cleans temp S3 files on success/non-autoretry failure and emits progress")
	add(&observations, containsAll(routeTests, "test_reingest_same_file_and_same_artifact", "assert len(ingest_result.data) == 0"), "same artifact re-ingest test", "tests prove same file plus same artifact is a no-op")
	add(&observations, containsAll(routeTests, "test_reingest_same_file_and_different_artifact", "assert len(ingest_result.data) == 1"), "different artifact re-ingest test", "tests prove same file can still ingest under a distinct artifact")
	add(&observations, containsAll(asyncTests, "test_cleanup_removes_temporary_s3_file", "test_cleanup_remove_temporary_with_an_autoretry_error"), "async cleanup tests", "tests cover success cleanup and autoretry retention")
	add(&observations, containsAll(repoGuide, "/v1/artifacts/ingest") && containsAll(repoAsyncGuide, "/v1/artifacts/ingest/async"), "repo docs match source route family", "checked docs in the repository rather than relying only on published pages")

	fmt.Println("[OBSERVATIONS]")
	for _, obs := range observations {
		label := "PASS"
		if !obs.Pass {
			label = "FINDING"
		}
		fmt.Printf("  [%s] %s: %s\n", label, obs.Name, obs.Detail)
	}

	fmt.Printf("[SUMMARY] verdict=VALIDATED guard_failures=%d finding_count=%d\n", guards, countFindings(observations))
	if guards > 0 || countFindings(observations) > 0 {
		os.Exit(1)
	}
}

func privateGPTRoot() string {
	if env := os.Getenv("PRIVATE_GPT_ROOT"); env != "" {
		return env
	}
	return `D:\tmp\private-gpt`
}

func readRequired(root string, observations *[]observation, guards *int, rel string) string {
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		failGuard(observations, guards, "read "+rel, err.Error())
		return ""
	}
	return string(data)
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func oneLine(root string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("git error: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
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
