package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const rawHTTPBodyLimit = 8 * 1024 * 1024

type rawHTTPResponse struct {
	body      []byte
	header    http.Header
	truncated bool
}

func (c *auraClient) doRaw(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (rawHTTPResponse, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return rawHTTPResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "*/*")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return rawHTTPResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, rawHTTPBodyLimit+1))
	if err != nil {
		return rawHTTPResponse{}, err
	}
	truncated := false
	if len(data) > rawHTTPBodyLimit {
		data = data[:rawHTTPBodyLimit]
		truncated = true
	}
	if resp.StatusCode >= 400 {
		return rawHTTPResponse{}, fmt.Errorf("aura %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return rawHTTPResponse{body: data, header: resp.Header.Clone(), truncated: truncated}, nil
}

func handleChatAnswer(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	questionID, err := requiredString(args, "question_id")
	if err != nil {
		return "", err
	}
	body := withoutKeys(args, "question_id")
	return doAuraJSON(ctx, c, http.MethodPost, "/api/chat/answer/"+url.PathEscape(questionID), nil, body)
}

func handleWikiGraph(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/wiki/graph", nil, nil)
}

func handleWikiGodNodes(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		TopK int `json:"top_k"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	q := url.Values{}
	if args.TopK > 0 {
		q.Set("top_k", fmt.Sprint(args.TopK))
	}
	return doAuraJSON(ctx, c, http.MethodGet, "/api/wiki/godnodes", q, nil)
}

func handleWikiPath(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		From    string `json:"from"`
		To      string `json:"to"`
		MaxHops int    `json:"max_hops"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.From) == "" || strings.TrimSpace(args.To) == "" {
		return "", errors.New("from and to are required")
	}
	q := url.Values{"from": {args.From}, "to": {args.To}}
	if args.MaxHops > 0 {
		q.Set("max_hops", fmt.Sprint(args.MaxHops))
	}
	return doAuraJSON(ctx, c, http.MethodGet, "/api/wiki/path", q, nil)
}

func handleWikiIndexRebuild(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodPost, "/api/wiki/index/rebuild", nil, map[string]any{})
}

func handleWikiReindex(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		DryRun bool `json:"dry_run"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	q := url.Values{}
	if args.DryRun {
		q.Set("dry_run", "1")
	}
	return doAuraJSON(ctx, c, http.MethodPost, "/api/wiki/reindex", q, map[string]any{})
}

func handleWikiLogAppend(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/wiki/log", "action")
}

func handleSourceGet(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return sourceIDEndpoint(ctx, c, raw, "", http.MethodGet)
}

func handleSourceOCR(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return sourceIDEndpoint(ctx, c, raw, "/ocr", http.MethodGet)
}

func handleSourceRaw(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	id, err := idArg(raw)
	if err != nil {
		return "", err
	}
	resp, err := c.doRaw(ctx, http.MethodGet, "/api/sources/"+url.PathEscape(id)+"/raw", nil, nil, "")
	if err != nil {
		return "", err
	}
	out := map[string]any{
		"id":                  id,
		"encoding":            "base64",
		"content_type":        resp.header.Get("Content-Type"),
		"content_disposition": resp.header.Get("Content-Disposition"),
		"bytes":               len(resp.body),
		"truncated":           resp.truncated,
		"content_base64":      base64.StdEncoding.EncodeToString(resp.body),
	}
	return marshalPretty(out)
}

func handleSourceDerived(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return sourceIDEndpoint(ctx, c, raw, "/derived", http.MethodGet)
}

func handleSourceUpload(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		FilePath string `json:"file_path"`
		Filename string `json:"filename"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.FilePath) == "" {
		return "", errors.New("file_path is required")
	}
	file, err := os.Open(args.FilePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	filename := strings.TrimSpace(args.Filename)
	if filename == "" {
		filename = filepath.Base(args.FilePath)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeMultipartFilename(filename)))
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	resp, err := c.doRaw(ctx, http.MethodPost, "/api/sources/upload", nil, &body, writer.FormDataContentType())
	if err != nil {
		return "", err
	}
	return prettyJSON(resp.body), nil
}

func handleSourceIngest(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postSourceAction(ctx, c, raw, "ingest")
}

func handleSourceReOCR(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postSourceAction(ctx, c, raw, "reocr")
}

func handleSourceDelete(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return sourceIDEndpoint(ctx, c, raw, "", http.MethodDelete)
}

func handleFileTree(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Root      string `json:"root"`
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	root, err := requiredRoot(args.Root)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	if args.Path != "" {
		q.Set("path", args.Path)
	}
	if args.Recursive {
		q.Set("recursive", "true")
	}
	return doAuraJSON(ctx, c, http.MethodGet, "/api/files/"+url.PathEscape(root)+"/tree", q, nil)
}

func handleFileRead(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return filePathEndpoint(ctx, c, raw, http.MethodGet, "/file", nil)
}

func handleFileWrite(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	root, err := requiredString(args, "root")
	if err != nil {
		return "", err
	}
	root, err = requiredRoot(root)
	if err != nil {
		return "", err
	}
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	body := withoutKeys(args, "root", "path")
	if _, ok := body["content"]; !ok {
		return "", errors.New("content is required")
	}
	q := url.Values{"path": {path}}
	return doAuraJSON(ctx, c, http.MethodPut, "/api/files/"+url.PathEscape(root)+"/file", q, body)
}

func handleFileDelete(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return filePathEndpoint(ctx, c, raw, http.MethodDelete, "/file", nil)
}

func handleFileDeleteMany(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	root, err := requiredString(args, "root")
	if err != nil {
		return "", err
	}
	root, err = requiredRoot(root)
	if err != nil {
		return "", err
	}
	body := withoutKeys(args, "root")
	return doAuraJSON(ctx, c, http.MethodPost, "/api/files/"+url.PathEscape(root)+"/delete-many", nil, body)
}

func handleFileMkdir(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return filePathEndpoint(ctx, c, raw, http.MethodPost, "/mkdir", map[string]any{})
}

func handleFileRename(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	root, err := requiredString(args, "root")
	if err != nil {
		return "", err
	}
	root, err = requiredRoot(root)
	if err != nil {
		return "", err
	}
	if _, err := requiredString(args, "from"); err != nil {
		return "", err
	}
	if _, err := requiredString(args, "to"); err != nil {
		return "", err
	}
	body := withoutKeys(args, "root")
	return doAuraJSON(ctx, c, http.MethodPost, "/api/files/"+url.PathEscape(root)+"/rename", nil, body)
}
