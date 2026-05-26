package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

func handleTaskGet(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	name, err := nameArg(raw)
	if err != nil {
		return "", err
	}
	return doAuraJSON(ctx, c, http.MethodGet, "/api/tasks/"+url.PathEscape(name), nil, nil)
}

func handleTaskUpsert(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/tasks", "name")
}

func handleTaskCancel(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postTaskAction(ctx, c, raw, "cancel")
}

func handleTaskDelete(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postTaskAction(ctx, c, raw, "delete")
}

func handleSkillGet(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	name, err := nameArg(raw)
	if err != nil {
		return "", err
	}
	return doAuraJSON(ctx, c, http.MethodGet, "/api/skills/"+url.PathEscape(name), nil, nil)
}

func handleSkillsCatalog(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	q := url.Values{}
	if args.Query != "" {
		q.Set("q", args.Query)
	}
	if args.Limit > 0 {
		q.Set("limit", fmt.Sprint(args.Limit))
	}
	return doAuraJSON(ctx, c, http.MethodGet, "/api/skills/catalog", q, nil)
}

func handleSkillInstall(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/skills/install", "source")
}

func handleSkillDelete(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postPathArgAction(ctx, c, raw, "name", "/api/skills", "delete")
}

func handleMCPStatus(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/mcp/status", nil, nil)
}

func handleMCPProviders(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/mcp/providers", nil, nil)
}

func handleMCPSetupMailGet(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/mcp/setup/mail", nil, nil)
}

func handleMCPSetupMailSave(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/mcp/setup/mail")
}

func handleMCPSetupDatabaseGet(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/mcp/setup/database", nil, nil)
}

func handleMCPSetupDatabaseSave(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/mcp/setup/database")
}

func handleMCPProviderProbe(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	id, err := idArg(raw)
	if err != nil {
		return "", err
	}
	return doAuraJSON(ctx, c, http.MethodPost, "/api/mcp/providers/"+url.PathEscape(id)+"/actions/probe", nil, map[string]any{})
}

func handleMCPProviderMailSearch(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postMCPProviderMail(ctx, c, raw, "search")
}

func handleMCPProviderMailRead(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postMCPProviderMail(ctx, c, raw, "read")
}

func handleMCPInvoke(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	server, err := requiredString(args, "server")
	if err != nil {
		return "", err
	}
	toolName, err := requiredString(args, "tool")
	if err != nil {
		return "", err
	}
	body := map[string]any{}
	if rawArgs, ok := args["arguments"]; ok {
		if rawArgs == nil {
			body = map[string]any{}
		} else if m, ok := rawArgs.(map[string]any); ok {
			body = m
		} else {
			return "", errors.New("arguments must be an object")
		}
	}
	return doAuraJSON(ctx, c, http.MethodPost, "/api/mcp/"+url.PathEscape(server)+"/tools/"+url.PathEscape(toolName), nil, body)
}

func handleToolCompact(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/tools/compact")
}

func handleToolRegistry(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/tools", nil, nil)
}

func handleToolCall(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	return postRawObject(ctx, c, raw, "/api/tools/call", "name")
}

func handleCompactReindex(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodPost, "/api/compact/reindex", nil, map[string]any{})
}

func handleToolAttempts(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	return doAuraJSON(ctx, c, http.MethodGet, "/api/maintenance/tool-attempts", nil, nil)
}

func doAuraJSON(ctx context.Context, c *auraClient, method, path string, query url.Values, body any) (string, error) {
	data, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func postRawObject(ctx context.Context, c *auraClient, raw json.RawMessage, path string, requiredKeys ...string) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	for _, key := range requiredKeys {
		if _, err := requiredString(args, key); err != nil {
			return "", err
		}
	}
	return doAuraJSON(ctx, c, http.MethodPost, path, nil, args)
}

func sourceIDEndpoint(ctx context.Context, c *auraClient, raw json.RawMessage, suffix, method string) (string, error) {
	id, err := idArg(raw)
	if err != nil {
		return "", err
	}
	return doAuraJSON(ctx, c, method, "/api/sources/"+url.PathEscape(id)+suffix, nil, nil)
}

func filePathEndpoint(ctx context.Context, c *auraClient, raw json.RawMessage, method, suffix string, body any) (string, error) {
	root, path, err := rootPathArgs(raw)
	if err != nil {
		return "", err
	}
	q := url.Values{"path": {path}}
	return doAuraJSON(ctx, c, method, "/api/files/"+url.PathEscape(root)+suffix, q, body)
}

func postPathArgAction(ctx context.Context, c *auraClient, raw json.RawMessage, argName, basePath, action string) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	value, err := requiredString(args, argName)
	if err != nil {
		return "", err
	}
	path := basePath + "/" + url.PathEscape(value) + "/" + action
	return doAuraJSON(ctx, c, http.MethodPost, path, nil, map[string]any{})
}

func postSourceAction(ctx context.Context, c *auraClient, raw json.RawMessage, action string) (string, error) {
	return postPathArgAction(ctx, c, raw, "id", "/api/sources", action)
}

func postTaskAction(ctx context.Context, c *auraClient, raw json.RawMessage, action string) (string, error) {
	return postPathArgAction(ctx, c, raw, "name", "/api/tasks", action)
}

func postMCPProviderMail(ctx context.Context, c *auraClient, raw json.RawMessage, action string) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	id, err := requiredString(args, "id")
	if err != nil {
		return "", err
	}
	body := withoutKeys(args, "id")
	return doAuraJSON(ctx, c, http.MethodPost, "/api/mcp/providers/"+url.PathEscape(id)+"/mail/"+action, nil, body)
}

func rootPathArgs(raw json.RawMessage) (string, string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", "", err
	}
	root, err := requiredString(args, "root")
	if err != nil {
		return "", "", err
	}
	root, err = requiredRoot(root)
	if err != nil {
		return "", "", err
	}
	path, err := requiredString(args, "path")
	if err != nil {
		return "", "", err
	}
	return root, path, nil
}

func requiredRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "wiki", "sources", "workspace", "skills":
		return value, nil
	default:
		return "", errors.New("root must be one of wiki, sources, workspace, skills")
	}
}

func idArg(raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	return requiredString(args, "id")
}

func nameArg(raw json.RawMessage) (string, error) {
	args, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	return requiredString(args, "name")
}

func decodeArgs(raw json.RawMessage, v any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		trimmed = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	return dec.Decode(v)
}

func rawObject(raw json.RawMessage) (map[string]any, error) {
	var args map[string]any
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

func withoutKeys(args map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

func requiredString(args map[string]any, key string) (string, error) {
	value, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s := strings.TrimSpace(fmt.Sprint(value))
	if s == "" || s == "<nil>" {
		return "", fmt.Errorf("%s is required", key)
	}
	return s, nil
}

func marshalPretty(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func escapeMultipartFilename(filename string) string {
	filename = filepath.Base(filename)
	filename = strings.ReplaceAll(filename, `\`, `\\`)
	filename = strings.ReplaceAll(filename, `"`, `\"`)
	return filename
}
