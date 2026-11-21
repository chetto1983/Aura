package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// call runs one method; a method this spike does not serve answers unknownMethod, so the log shows
// what a facade would still need.
func (s *store) call(name string, args map[string]any, id string) [][]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := args["accountId"]; ok && a != s.account && name != "Core/echo" {
		return [][]any{{"error", map[string]any{"type": "accountNotFound"}, id}}
	}
	switch name {
	case "Core/echo":
		return [][]any{{name, args, id}}
	case "Mailbox/get":
		return [][]any{{name, s.mailboxGet(args), id}}
	case "Email/query":
		return [][]any{{name, s.emailQuery(args), id}}
	case "Email/get":
		return [][]any{{name, s.emailGet(args), id}}
	case "Email/set":
		return [][]any{{name, s.emailSet(args), id}}
	case "Email/import":
		return [][]any{{name, s.emailImport(args), id}}
	case "Identity/get":
		return [][]any{{name, map[string]any{"accountId": s.account, "state": "id-1", "notFound": []string{},
			"list": []any{map[string]any{"id": "I1", "name": "Ada", "email": s.owner, "replyTo": nil, "bcc": nil,
				"textSignature": "", "htmlSignature": "", "mayDelete": false}}}, id}}
	case "EmailSubmission/set":
		return s.submissionSet(args, id)
	case "Thread/get":
		list := []any{}
		for _, t := range strings2(args["ids"]) {
			list = append(list, map[string]any{"id": t, "emailIds": []string{strings.Replace(t, "T", "M", 1)}})
		}
		return [][]any{{name, map[string]any{"accountId": s.account, "state": s.stateString(), "list": list, "notFound": []string{}}, id}}
	}
	return [][]any{{"error", map[string]any{"type": "unknownMethod"}, id}}
}

func (s *store) mailboxGet(args map[string]any) map[string]any {
	want := strings2(args["ids"])
	list := []any{}
	for _, m := range s.mailboxes {
		if args["ids"] != nil && !slices.Contains(want, m.ID) {
			continue
		}
		total, unread := 0, 0
		for _, e := range s.inMailbox(m.ID) {
			total++
			if !e.Keywords["$seen"] {
				unread++
			}
		}
		list = append(list, map[string]any{"id": m.ID, "name": m.Name, "parentId": nil, "role": m.Role, "sortOrder": m.Sort,
			"totalEmails": total, "unreadEmails": unread, "totalThreads": total, "unreadThreads": unread, "isSubscribed": true,
			"myRights": map[string]any{"mayReadItems": true, "mayAddItems": true, "mayRemoveItems": true, "maySetSeen": true,
				"maySetKeywords": true, "mayCreateChild": false, "mayRename": false, "mayDelete": false, "maySubmit": true}})
	}
	return map[string]any{"accountId": s.account, "state": s.stateString(), "list": list, "notFound": []string{}}
}

func (s *store) emailQuery(args map[string]any) map[string]any {
	filter, _ := args["filter"].(map[string]any)
	in, _ := filter["inMailbox"].(string)
	all := s.inMailbox(in)
	position, limit := num(args["position"], 0), num(args["limit"], 500)
	if position < 0 {
		position = max(0, len(all)+position)
	}
	ids := []string{}
	for i := position; i < len(all) && len(ids) < limit; i++ {
		ids = append(ids, all[i].ID)
	}
	out := map[string]any{"accountId": s.account, "queryState": s.stateString(), "canCalculateChanges": false,
		"position": position, "ids": ids, "limit": limit}
	if args["calculateTotal"] == true {
		out["total"] = len(all)
	}
	return out
}

func (s *store) emailGet(args map[string]any) map[string]any {
	props := strings2(args["properties"])
	list := []any{}
	notFound := []string{}
	for _, id := range strings2(args["ids"]) {
		e := s.emails[id]
		if e == nil {
			notFound = append(notFound, id)
			continue
		}
		list = append(list, s.emailObject(e, props))
	}
	return map[string]any{"accountId": s.account, "state": s.stateString(), "list": list, "notFound": notFound}
}

func (s *store) emailObject(e *email, props []string) map[string]any {
	h, body := header(e.Raw)
	all := map[string]any{"id": e.ID, "blobId": e.Blob, "threadId": strings.Replace(e.ID, "M", "T", 1),
		"mailboxIds": e.Mailboxes, "keywords": e.Keywords, "size": len(e.Raw), "receivedAt": e.ReceivedAt.Format(time.RFC3339),
		"hasAttachment": false, "preview": preview(body), "subject": h.Get("Subject"), "from": addresses(h, "From"),
		"to": addresses(h, "To"), "cc": addresses(h, "Cc"), "bcc": addresses(h, "Bcc"), "replyTo": addresses(h, "Reply-To"),
		"sender": addresses(h, "Sender"), "messageId": []string{strings.Trim(h.Get("Message-ID"), "<>")},
		"inReplyTo": nil, "references": nil, "attachments": []any{},
		"textBody":   []any{map[string]any{"partId": "1", "blobId": e.Blob, "type": "text/plain", "size": len(body), "charset": "utf-8"}},
		"htmlBody":   []any{map[string]any{"partId": "1", "blobId": e.Blob, "type": "text/plain", "size": len(body), "charset": "utf-8"}},
		"bodyValues": map[string]any{"1": map[string]any{"value": body, "isEncodingProblem": false, "isTruncated": false}}}
	if d, err := h.Date(); err == nil {
		all["sentAt"] = d.Format(time.RFC3339)
	}
	var headers []any
	for k, vs := range h {
		for _, v := range vs {
			headers = append(headers, map[string]any{"name": k, "value": " " + v})
		}
	}
	all["headers"] = headers
	if len(props) == 0 {
		props = []string{"id", "blobId", "threadId", "mailboxIds", "keywords", "size", "receivedAt", "messageId", "inReplyTo",
			"references", "sender", "from", "to", "cc", "bcc", "replyTo", "subject", "sentAt", "hasAttachment", "preview",
			"bodyValues", "textBody", "htmlBody", "attachments"}
	}
	out := map[string]any{"id": e.ID}
	for _, p := range props {
		if field, ok := strings.CutPrefix(p, "header:"); ok {
			name, _, _ := strings.Cut(field, ":")
			out[p] = h.Get(name)
			continue
		}
		out[p] = all[p]
	}
	return out
}

func (s *store) emailSet(args map[string]any) map[string]any {
	out := map[string]any{"accountId": s.account, "oldState": s.stateString()}
	updated, notUpdated := map[string]any{}, map[string]any{}
	if update, ok := args["update"].(map[string]any); ok {
		for id, raw := range update {
			e := s.emails[id]
			patch, _ := raw.(map[string]any)
			if e == nil || patch == nil {
				notUpdated[id] = map[string]any{"type": "notFound"}
				continue
			}
			for path, v := range patch {
				switch {
				case path == "keywords":
					e.Keywords = boolSet(v)
				case path == "mailboxIds":
					e.Mailboxes = boolSet(v)
				case strings.HasPrefix(path, "keywords/"):
					setOrClear(e.Keywords, strings.TrimPrefix(path, "keywords/"), v)
				case strings.HasPrefix(path, "mailboxIds/"):
					setOrClear(e.Mailboxes, strings.TrimPrefix(path, "mailboxIds/"), v)
				default:
					notUpdated[id] = map[string]any{"type": "invalidPatch", "description": path}
				}
			}
			updated[id] = nil
			s.state++
		}
	}
	destroyed := []string{}
	for _, id := range strings2(args["destroy"]) {
		if e := s.emails[id]; e != nil {
			delete(s.emails, id)
			destroyed = append(destroyed, id)
			s.state++
		}
	}
	out["updated"], out["notUpdated"], out["destroyed"] = updated, notUpdated, destroyed
	if create, ok := args["create"].(map[string]any); ok && len(create) > 0 {
		notCreated := map[string]any{}
		for cid := range create {
			notCreated[cid] = map[string]any{"type": "forbidden", "description": "the spike makes no drafts on the server"}
		}
		out["notCreated"] = notCreated
	}
	out["newState"] = s.stateString()
	return out
}

func (s *store) emailImport(args map[string]any) map[string]any {
	out := map[string]any{"accountId": s.account, "oldState": s.stateString()}
	created, notCreated := map[string]any{}, map[string]any{}
	emails, _ := args["emails"].(map[string]any)
	for cid, raw := range emails {
		spec, _ := raw.(map[string]any)
		blob := s.blobs[fmt.Sprint(spec["blobId"])]
		if blob == nil {
			notCreated[cid] = map[string]any{"type": "blobNotFound"}
			continue
		}
		e := s.add(blob, boolSet(spec["mailboxIds"]), boolSet(spec["keywords"]), time.Now().UTC())
		created[cid] = map[string]any{"id": e.ID, "blobId": e.Blob, "threadId": strings.Replace(e.ID, "M", "T", 1), "size": len(blob)}
	}
	out["created"], out["notCreated"], out["newState"] = created, notCreated, s.stateString()
	return out
}

// submissionSet "sends" by recording the submission: what Aura would turn into an approval request.
func (s *store) submissionSet(args map[string]any, id string) [][]any {
	created := map[string]any{}
	creates, _ := args["create"].(map[string]any)
	for cid, raw := range creates {
		_ = raw
		s.next++
		created[cid] = map[string]any{"id": fmt.Sprintf("S%d", s.next), "undoStatus": "final", "sendAt": time.Now().UTC().Format(time.RFC3339)}
	}
	out := [][]any{{"EmailSubmission/set", map[string]any{"accountId": s.account, "oldState": "sub-1", "newState": "sub-2",
		"created": created, "notCreated": map[string]any{}}, id}}
	if onSuccess, ok := args["onSuccessUpdateEmail"].(map[string]any); ok {
		update := map[string]any{}
		for ref, patch := range onSuccess {
			if _, ok := created[strings.TrimPrefix(ref, "#")]; ok {
				if spec, ok := creates[strings.TrimPrefix(ref, "#")].(map[string]any); ok {
					update[fmt.Sprint(spec["emailId"])] = patch
				}
			}
		}
		out = append(out, []any{"Email/set", s.emailSet(map[string]any{"update": update}), id})
	}
	return out
}

func strings2(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func num(v any, fallback int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return fallback
}

func boolSet(v any) map[string]bool {
	out := map[string]bool{}
	if m, ok := v.(map[string]any); ok {
		for k, b := range m {
			if b == true {
				out[k] = true
			}
		}
	}
	return out
}

func setOrClear(m map[string]bool, key string, v any) {
	if v == true {
		m[key] = true
	} else {
		delete(m, key)
	}
}
