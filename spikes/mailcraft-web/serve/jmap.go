package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	capCore       = "urn:ietf:params:jmap:core"
	capMail       = "urn:ietf:params:jmap:mail"
	capSubmission = "urn:ietf:params:jmap:submission"
)

// jmapServer answers the part of RFC 8620/8621 a client asks for and writes every exchange to a
// JSON Lines file: the measurement of what MailCraft's browser build calls.
type jmapServer struct {
	origin string
	store  *store
	mu     sync.Mutex
	out    *os.File
}

func newJMAP(origin string, s *store, path string) (*jmapServer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- the spike's own log
	if err != nil {
		return nil, err
	}
	return &jmapServer{origin: origin, store: s, out: f}, nil
}

func (j *jmapServer) record(entry map[string]any) {
	entry["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	line, _ := json.Marshal(entry)
	j.mu.Lock()
	defer j.mu.Unlock()
	_, _ = j.out.Write(append(line, '\n'))
}

func (j *jmapServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+token {
		j.record(map[string]any{"http": r.Method + " " + r.URL.String(), "status": 401})
		w.Header().Set("WWW-Authenticate", `Bearer realm="spike"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/jmap/session":
		j.record(map[string]any{"http": "GET /jmap/session"})
		writeJSON(w, http.StatusOK, j.session())
	case r.Method == http.MethodPost && r.URL.Path == "/jmap/api":
		j.api(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/jmap/upload/"):
		j.upload(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/jmap/download/"):
		j.download(w, r)
	default:
		j.record(map[string]any{"http": r.Method + " " + r.URL.String(), "status": 404})
		http.NotFound(w, r)
	}
}

func (j *jmapServer) session() map[string]any {
	s := j.store
	return map[string]any{
		"capabilities": map[string]any{
			capCore: map[string]any{"maxSizeUpload": 50_000_000, "maxConcurrentUpload": 4, "maxSizeRequest": 10_000_000,
				"maxConcurrentRequests": 4, "maxCallsInRequest": 16, "maxObjectsInGet": 500, "maxObjectsInSet": 500,
				"collationAlgorithms": []string{}},
			capMail:       map[string]any{},
			capSubmission: map[string]any{},
		},
		"accounts": map[string]any{s.account: map[string]any{"name": s.owner, "isPersonal": true, "isReadOnly": false,
			"accountCapabilities": map[string]any{
				capMail: map[string]any{"maxMailboxesPerEmail": nil, "maxMailboxDepth": 10, "maxSizeMailboxName": 255,
					"maxSizeAttachmentsPerEmail": 50_000_000, "emailQuerySortOptions": []string{"receivedAt"}, "mayCreateTopLevelMailbox": false},
				capSubmission: map[string]any{"maxDelayedSend": 0, "submissionExtensions": map[string]any{}},
			}}},
		"primaryAccounts": map[string]any{capMail: s.account, capSubmission: s.account},
		"username":        s.owner,
		"apiUrl":          j.origin + "/jmap/api",
		"downloadUrl":     j.origin + "/jmap/download/{accountId}/{blobId}/{name}?type={type}",
		"uploadUrl":       j.origin + "/jmap/upload/{accountId}/",
		"eventSourceUrl":  j.origin + "/jmap/eventsource/?types={types}&closeafter={closeafter}&ping={ping}",
		"state":           "session-1",
	}
}

type request struct {
	Using       []string          `json:"using"`
	MethodCalls []json.RawMessage `json:"methodCalls"`
}

func (j *jmapServer) api(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10_000_000))
	var req request
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"type": "urn:ietf:params:jmap:error:notJSON", "status": 400})
		return
	}
	var responses [][]any
	for _, raw := range req.MethodCalls {
		var call []json.RawMessage
		var name, id string
		var args map[string]any
		if json.Unmarshal(raw, &call) != nil || len(call) != 3 || json.Unmarshal(call[0], &name) != nil ||
			json.Unmarshal(call[1], &args) != nil || json.Unmarshal(call[2], &id) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"type": "urn:ietf:params:jmap:error:notRequest", "status": 400})
			return
		}
		resolved, refErr := resolveRefs(args, responses)
		var out [][]any
		if refErr != nil {
			out = [][]any{{"error", map[string]any{"type": "invalidResultReference", "description": refErr.Error()}, id}}
		} else {
			out = normalize(j.store.call(name, resolved, id))
		}
		j.record(map[string]any{"call": name, "args": args, "answer": out[0][0], "error": errorType(out[0])})
		responses = append(responses, out...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"methodResponses": responses, "sessionState": "session-1"})
}

// normalize gives a method's responses the shape a client receives (JSON's own types), so that a
// later back-reference reads them as it would read the wire.
func normalize(out [][]any) [][]any {
	raw, err := json.Marshal(out)
	if err != nil {
		return out
	}
	var back [][]any
	if json.Unmarshal(raw, &back) != nil {
		return out
	}
	return back
}

func errorType(response []any) any {
	if response[0] != "error" {
		return nil
	}
	return response[1].(map[string]any)["type"]
}

// resolveRefs replaces every "#name" argument with the result it points at (RFC 8620 §3.7).
func resolveRefs(args map[string]any, done [][]any) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range args {
		if !strings.HasPrefix(k, "#") {
			out[k] = v
			continue
		}
		ref, _ := v.(map[string]any)
		var source any
		for _, r := range done {
			if r[2] == ref["resultOf"] && r[0] == ref["name"] {
				source = r[1]
			}
		}
		if source == nil {
			return nil, fmt.Errorf("no result for %v", ref)
		}
		path, _ := ref["path"].(string)
		value, err := pointer(source, strings.Split(strings.TrimPrefix(path, "/"), "/"))
		if err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(k, "#")] = value
	}
	return out, nil
}

// pointer follows an RFC 6901 path, where "*" maps over a list and flattens nested lists.
func pointer(v any, path []string) (any, error) {
	if len(path) == 0 || (len(path) == 1 && path[0] == "") {
		return v, nil
	}
	head, rest := path[0], path[1:]
	switch t := v.(type) {
	case map[string]any:
		return pointer(t[head], rest)
	case []any:
		if head != "*" {
			return nil, fmt.Errorf("index %q into a list", head)
		}
		var flat []any
		for _, item := range t {
			got, err := pointer(item, rest)
			if err != nil {
				return nil, err
			}
			if list, ok := got.([]any); ok {
				flat = append(flat, list...)
			} else {
				flat = append(flat, got)
			}
		}
		return flat, nil
	case []string:
		items := make([]any, len(t))
		for i, s := range t {
			items[i] = s
		}
		return pointer(items, path)
	}
	return nil, fmt.Errorf("path %v does not resolve", path)
}

func (j *jmapServer) upload(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 50_000_000))
	s := j.store
	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("U%d", s.next)
	s.blobs[id] = body
	s.mu.Unlock()
	j.record(map[string]any{"http": "POST " + r.URL.Path, "bytes": len(body), "type": r.Header.Get("Content-Type")})
	writeJSON(w, http.StatusCreated, map[string]any{"accountId": s.account, "blobId": id, "type": r.Header.Get("Content-Type"), "size": len(body)})
}

func (j *jmapServer) download(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jmap/download/"), "/")
	j.store.mu.Lock()
	var blob []byte
	if len(parts) >= 2 {
		blob = j.store.blobs[parts[1]]
	}
	j.store.mu.Unlock()
	j.record(map[string]any{"http": "GET " + r.URL.String(), "found": blob != nil})
	if blob == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(blob)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Print(err)
	}
}
