package display

import (
	"bytes"
	"encoding/json"
	"strings"
)

// These field lists follow the pinned curated sidecar result builders. Only
// completed reads whose host marker, action and result shape all match become
// tables. Views, details, attachments and writes keep their raw receipts.
type sidecarField struct {
	key, label       string
	optional, joined bool
}

type sidecarSpec struct {
	root, title string
	fields      []sidecarField
	single      bool
}

var calendarReadSpecs = map[string]sidecarSpec{
	"list_accounts": {root: "accounts", title: "calendar_accounts", fields: []sidecarField{
		{key: "accountId", label: "Account"}, {key: "provider", label: "Provider"}, {key: "displayName", label: "Name", optional: true},
	}},
	"get_emails": {root: "emails", title: "calendar_emails", fields: []sidecarField{
		{key: "id", label: "Email"}, {key: "accountId", label: "Account"}, {key: "subject", label: "Subject", optional: true},
		{key: "from", label: "From", optional: true}, {key: "receivedDateTime", label: "Received", optional: true},
	}},
	"search_emails": {root: "emails", title: "calendar_emails", fields: []sidecarField{
		{key: "id", label: "Email"}, {key: "accountId", label: "Account"}, {key: "subject", label: "Subject", optional: true},
		{key: "from", label: "From", optional: true}, {key: "receivedDateTime", label: "Received", optional: true},
	}},
	"list_calendars": {root: "calendars", title: "calendar_calendars", fields: []sidecarField{
		{key: "id", label: "Calendar"}, {key: "accountId", label: "Account"}, {key: "name", label: "Name", optional: true},
		{key: "owner", label: "Owner", optional: true},
	}},
	"get_calendar_events": {root: "events", title: "calendar_events", fields: []sidecarField{
		{key: "eventId", label: "Event"}, {key: "accountId", label: "Account"}, {key: "subject", label: "Subject", optional: true},
		{key: "start_local", label: "Start", optional: true}, {key: "end_local", label: "End", optional: true},
	}},
	"get_contacts": {root: "contacts", title: "calendar_contacts", fields: []sidecarField{
		{key: "id", label: "Contact"}, {key: "accountId", label: "Account"}, {key: "displayName", label: "Name", optional: true},
		{key: "emailAddresses", label: "Email", optional: true, joined: true},
	}},
	"search_contacts": {root: "contacts", title: "calendar_contacts", fields: []sidecarField{
		{key: "id", label: "Contact"}, {key: "accountId", label: "Account"}, {key: "displayName", label: "Name", optional: true},
		{key: "emailAddresses", label: "Email", optional: true, joined: true},
	}},
}

var whatsappReadSpecs = map[string]sidecarSpec{
	"list_chats":                 whatsappChatSpec(false),
	"get_contact_chats":          whatsappChatSpec(false),
	"get_chat":                   whatsappChatSpec(true),
	"get_direct_chat_by_contact": whatsappChatSpec(true),
	"list_messages":              whatsappMessageSpec(false),
	"get_last_interaction":       whatsappMessageSpec(true),
	"search_contacts":            whatsappContactSpec(false),
	"get_contact":                whatsappContactSpec(true),
}

func whatsappChatSpec(single bool) sidecarSpec {
	return sidecarSpec{title: "whatsapp_chats", single: single, fields: []sidecarField{
		{key: "jid", label: "Chat"}, {key: "name", label: "Name", optional: true},
		{key: "last_message_time", label: "Last active", optional: true}, {key: "last_message", label: "Last message", optional: true},
	}}
}

func whatsappMessageSpec(single bool) sidecarSpec {
	return sidecarSpec{title: "whatsapp_messages", single: single, fields: []sidecarField{
		{key: "timestamp", label: "When"}, {key: "sender_display", label: "Sender", optional: true},
		{key: "chat_jid", label: "Chat"}, {key: "content", label: "Message", optional: true},
	}}
}

func whatsappContactSpec(single bool) sidecarSpec {
	return sidecarSpec{title: "whatsapp_contacts", single: single, fields: []sidecarField{
		{key: "jid", label: "Contact"}, {key: "name", label: "Name", optional: true},
		{key: "phone_number", label: "Phone", optional: true},
	}}
}

func normalizeSidecarPreview(in PreviewInput) (Payload, bool) {
	marker := in.TrustedMCP
	if marker == nil || len(in.ResultPreview) > maxMemoryPreviewBytes || !strings.HasSuffix(in.ToolName, "__"+marker.Tool) {
		return Payload{}, false
	}
	var spec sidecarSpec
	var ok bool
	switch marker.Recipe {
	case "recipe:calendar":
		if marker.Tool != "calendar" {
			return Payload{}, false
		}
		var arguments struct {
			Action string `json:"action"`
		}
		if json.Unmarshal([]byte(in.Arguments), &arguments) != nil || arguments.Action != marker.Action {
			return Payload{}, false
		}
		spec, ok = calendarReadSpecs[marker.Action]
	case "recipe:whatsapp":
		if marker.Action != "" {
			return Payload{}, false
		}
		spec, ok = whatsappReadSpecs[marker.Tool]
	}
	if !ok {
		return Payload{}, false
	}
	var rawRows []json.RawMessage
	if spec.root != "" {
		var body map[string]json.RawMessage
		if json.Unmarshal([]byte(in.ResultPreview), &body) != nil || body == nil {
			return Payload{}, false
		}
		warnings := bytes.TrimSpace(body["warnings"])
		if len(warnings) > 0 && !bytes.Equal(warnings, []byte("null")) {
			var entries []json.RawMessage
			if json.Unmarshal(warnings, &entries) != nil || len(entries) > 0 {
				return Payload{}, false
			}
		}
		if json.Unmarshal(body[spec.root], &rawRows) != nil || rawRows == nil {
			return Payload{}, false
		}
	} else if spec.single {
		if !json.Valid([]byte(in.ResultPreview)) {
			return Payload{}, false
		}
		rawRows = []json.RawMessage{json.RawMessage(in.ResultPreview)}
	} else if json.Unmarshal([]byte(in.ResultPreview), &rawRows) != nil || rawRows == nil {
		return Payload{}, false
	}
	rows := make([][]string, 0, min(len(rawRows), maxMemoryRows))
	for _, raw := range rawRows[:min(len(rawRows), maxMemoryRows)] {
		var data map[string]json.RawMessage
		if json.Unmarshal(raw, &data) != nil || data == nil {
			return Payload{}, false
		}
		row := make([]string, 0, len(spec.fields))
		for _, field := range spec.fields {
			value, valid := sidecarCell(data[field.key], field)
			if !valid {
				return Payload{}, false
			}
			row = append(row, value)
		}
		rows = append(rows, row)
	}
	columns := make([]string, 0, len(spec.fields))
	for _, field := range spec.fields {
		columns = append(columns, field.label)
	}
	return Payload{Type: KindTable, ToolCallID: in.ToolCallID, Title: spec.title, Table: &Table{
		Columns: columns, Rows: rows, OmittedRows: max(0, len(rawRows)-len(rows)),
	}}, true
}

func sidecarCell(raw json.RawMessage, field sidecarField) (string, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", field.optional
	}
	if field.joined {
		var values []string
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return "", false
		}
		return memoryCell(strings.Join(values[:min(len(values), 4)], ", ")), true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || (!field.optional && value == "") {
		return "", false
	}
	return memoryCell(value), true
}
