package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
)

// sourceIDRe extracts a source_id token (`src_<16 hex>`) from free-form
// assistant text. The doc tool's JSON reply contains source_id and the
// model usually echoes it; we grep the natural-language reply for it
// rather than parse a structured field that may or may not exist.
var sourceIDRe = regexp.MustCompile(`src_[a-f0-9]{16}`)

// findSourceID returns the first src_xxx token in s, or "" if absent.
func findSourceID(s string) string {
	return sourceIDRe.FindString(s)
}

// findSourceByFilename queries /api/sources for the most recent source
// whose filename matches the supplied substring. Used by doc probes
// because the model occasionally fabricates the source_id in the reply
// (real failure mode observed 2026-05-12: tool fires correctly, but
// the assistant echoes a hallucinated ID). The filesystem ground truth
// is the authoritative source list — read it instead of parsing prose.
func (e *Env) findSourceByFilename(substr string) (string, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var sources []struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
		return "", fmt.Errorf("decode sources: %w", err)
	}
	for _, s := range sources {
		if strings.Contains(s.Filename, substr) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no source matched %q (have %d sources)", substr, len(sources))
}

// fetchSourceRaw returns the raw bytes of a source via /api/sources/{id}/raw.
func (e *Env) fetchSourceRaw(id string) ([]byte, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id + "/raw"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return io.ReadAll(resp.Body)
}

// uploadSourceFile POSTs a binary to /api/sources/upload (multipart) and
// returns the assigned source id. This is the bytes-of-the-artifact entry
// point — the LLM is bypassed so Wave 2.9 markitdown probes can verify the
// extract pipeline directly, without entangling the test with model
// behavior. See [docs/wave-2.9-markitdown.md](docs/wave-2.9-markitdown.md).
func (e *Env) uploadSourceFile(filename, mimeType string, body []byte) (id, status string, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	hdr.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return "", "", fmt.Errorf("multipart create part: %w", err)
	}
	if _, err := part.Write(body); err != nil {
		return "", "", fmt.Errorf("multipart write: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", "", fmt.Errorf("multipart close: %w", err)
	}
	url := strings.TrimRight(e.APIBase, "/") + "/sources/upload"
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var ur struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &ur); err != nil {
		return "", "", fmt.Errorf("decode upload response: %w", err)
	}
	return ur.ID, ur.Status, nil
}

// fetchSourceMarkdown returns the on-disk extract.md (or ocr.md for PDF
// sources) for a given source id via /api/sources/{id}/markdown. The
// endpoint reads the bytes off the filesystem inside the container, so this
// is the disk-byte ground truth even when the wiki lives in a named volume
// that the host can't bind-mount.
func (e *Env) fetchSourceMarkdown(id string) (string, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id + "/markdown"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var sm struct {
		Markdown string `json:"markdown"`
		File     string `json:"file"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sm); err != nil {
		return "", fmt.Errorf("decode markdown response: %w", err)
	}
	return sm.Markdown, nil
}

// deleteSource removes a source via DELETE /api/sources/{id}. Used by Cleanup
// hooks so each probe run starts with an empty wiki/raw for the test ID.
func (e *Env) deleteSource(id string) error {
	url := strings.TrimRight(e.APIBase, "/") + "/sources/" + id
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return nil
}

// writeWikiFile writes a top-level wiki file through the dashboard file API so
// the production reindex hook runs in the same path operators use.
func (e *Env) writeWikiFile(path, content string) error {
	body, _ := json.Marshal(map[string]string{
		"content":  content,
		"encoding": "utf-8",
	})
	url := strings.TrimRight(e.APIBase, "/") + "/files/wiki/file?path=" + path
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return nil
}

// deleteWikiFile removes a top-level wiki file through the dashboard file API.
func (e *Env) deleteWikiFile(path string) error {
	url := strings.TrimRight(e.APIBase, "/") + "/files/wiki/file?path=" + path
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return nil
}

// fetchWikiPage reads a single wiki page through the dashboard API.
// Returns (nil, true) when the API responds 404 (page genuinely missing)
// so Verify functions can distinguish "missing" from transport errors.
func (e *Env) fetchWikiPage(slug string) (body string, missing bool, err error) {
	url := strings.TrimRight(e.APIBase, "/") + "/wiki/page?slug=" + slug
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", true, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var page struct {
		BodyMD string `json:"body_md"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return "", false, fmt.Errorf("decode: %w", err)
	}
	// Reconstruct a frontmatter-ish view so substring asserts work against title.
	return "title: " + page.Title + "\n" + page.BodyMD, false, nil
}

func (e *Env) fetchTask(name string) (kind, status string, missing bool, err error) {
	url := strings.TrimRight(e.APIBase, "/") + "/tasks/" + url.PathEscape(name)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", true, nil
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var task struct {
		Kind   string `json:"kind"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return "", "", false, fmt.Errorf("decode task: %w", err)
	}
	return task.Kind, task.Status, false, nil
}

func (e *Env) cancelTask(name string) error {
	url := strings.TrimRight(e.APIBase, "/") + "/tasks/" + url.PathEscape(name) + "/cancel"
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return nil
}

// fetchSandboxEnabled calls /api/health and returns whether the sandbox is enabled.
func (e *Env) fetchSandboxEnabled() (bool, error) {
	healthURL := strings.TrimRight(e.APIBase, "/") + "/health"
	req, _ := http.NewRequest(http.MethodGet, healthURL, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("GET %s: %w", healthURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var h struct {
		Sandbox struct {
			Enabled bool `json:"enabled"`
		} `json:"sandbox"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return false, fmt.Errorf("decode health response: %w", err)
	}
	return h.Sandbox.Enabled, nil
}

func (e *Env) fetchUserID() (string, error) {
	url := strings.TrimRight(e.APIBase, "/") + "/auth/whoami"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+e.APIToken)
	resp, err := e.APIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var who struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&who); err != nil {
		return "", fmt.Errorf("decode whoami: %w", err)
	}
	return strings.TrimSpace(who.UserID), nil
}

func (e *Env) sendChatThread(threadID, prompt string) (ChatReply, error) {
	chatURL := strings.TrimRight(e.APIBase, "/") + "/chat"
	return sendChatWithThread(e.APIClient, chatURL, e.APIToken, prompt, threadID)
}

func sendChat(client *http.Client, baseURL, token, prompt string) (ChatReply, error) {
	return sendChatWithThread(client, baseURL, token, prompt, "")
}

func sendChatWithThread(client *http.Client, baseURL, token, prompt, threadID string) (ChatReply, error) {
	var reply ChatReply
	for attempt := 0; attempt < 2; attempt++ {
		got, err := sendChatOnce(client, baseURL, token, prompt, threadID)
		if err != nil {
			return ChatReply{}, err
		}
		reply = got
		if !isEmptyProviderReply(reply) {
			return reply, nil
		}
		fmt.Printf("[probe_chat] retrying empty provider reply for prompt %q\n", truncate(prompt, 80))
	}
	return reply, nil
}

func sendChatOnce(client *http.Client, baseURL, token, prompt, threadID string) (ChatReply, error) {
	payloadMap := map[string]string{"message": prompt}
	if strings.TrimSpace(threadID) != "" {
		payloadMap["thread_id"] = strings.TrimSpace(threadID)
	}
	payload, _ := json.Marshal(payloadMap)
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
	if err != nil {
		return ChatReply{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ChatReply{}, fmt.Errorf("POST %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatReply{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ChatReply{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	var reply ChatReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return ChatReply{}, fmt.Errorf("decode reply: %w (raw: %s)", err, truncate(string(body), 400))
	}
	return reply, nil
}

// sendChatDenyCheck posts to /api/chat with an arbitrary bearer token and
// returns whether the server responded with HTTP 403. Used by the
// web-capability-deny probe to verify that an actor without api.chat grants
// is refused before the agent loop runs.
func (e *Env) sendChatDenyCheck(token, message string) (forbidden bool, body string, err error) {
	chatURL := strings.TrimRight(e.APIBase, "/") + "/chat"
	payload, _ := json.Marshal(map[string]string{"message": message})
	req, reqErr := http.NewRequest(http.MethodPost, chatURL, bytes.NewReader(payload))
	if reqErr != nil {
		return false, "", fmt.Errorf("build request: %w", reqErr)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, respErr := e.APIClient.Do(req)
	if respErr != nil {
		return false, "", fmt.Errorf("POST %s: %w", chatURL, respErr)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode == http.StatusForbidden, string(raw), nil
}

func isEmptyProviderReply(reply ChatReply) bool {
	text := strings.TrimSpace(reply.Reply)
	// Budget-exhaustion fallback: always treat as empty regardless of LLMCalls
	// count — graceful finalize (US-FIX01) may add an extra LLM call before
	// falling back to this string.
	if text == "" || text == "I reached the per-turn budget without a usable result." {
		return true
	}
	// Zero-content single-LLM-call with no tool activity: provider returned nothing.
	return reply.ToolCalls == 0 && reply.Tokens == 0 && reply.LLMCalls == 1
}
