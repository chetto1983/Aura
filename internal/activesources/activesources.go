// Package activesources is the wire codec of X-Aura-Active-Sources: the (conversation, turn)
// pairs the agent bridge (internal/agent/mcptools) sends cmd/arcadedb-mcp so a recall does not
// return the conversation it is answering inside.
//
// Both ends used to carry their own copy of this codec. The server's said it was "defined
// independently because this binary and the agent bridge share a loopback wire contract, not
// a Go package boundary", and that the bounds "must stay byte-identical on both sides". Both
// live in this module, and no test compared the copies (2026-10-26). One package makes the
// byte-identity structural.
package activesources

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Header is the HTTP header the bridge sets and the server reads.
const Header = "X-Aura-Active-Sources"

// MaxSources is the most pairs one header may carry.
const MaxSources = 8

const (
	version        = 1
	maxIDRunes     = 256
	maxDecodedSize = 1536
	maxEncodedSize = 2048
)

// Source is one active (conversation, turn) pair. Both ids are host-owned.
type Source struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
}

type envelope struct {
	Version int      `json:"version"`
	Sources []Source `json:"sources"`
}

// Encode returns the canonical unpadded base64url JSON of sources. Sorting is part of the wire
// contract, so equivalent source sets have exactly one encoding.
func Encode(sources []Source) (string, error) {
	if len(sources) == 0 || len(sources) > MaxSources {
		return "", fmt.Errorf("active-source payload must contain between 1 and %d entries", MaxSources)
	}
	canonical := append([]Source(nil), sources...)
	for _, source := range canonical {
		if err := validate(source); err != nil {
			return "", err
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].ConversationID != canonical[j].ConversationID {
			return canonical[i].ConversationID < canonical[j].ConversationID
		}
		return canonical[i].TurnID < canonical[j].TurnID
	})
	for index := 1; index < len(canonical); index++ {
		if canonical[index] == canonical[index-1] {
			return "", errors.New("active-source payload contains a duplicate")
		}
	}
	raw, err := json.Marshal(envelope{Version: version, Sources: canonical})
	if err != nil {
		return "", fmt.Errorf("encode active-source payload: %w", err)
	}
	if len(raw) > maxDecodedSize {
		return "", fmt.Errorf("active-source payload exceeds %d bytes", maxDecodedSize)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > maxEncodedSize {
		return "", fmt.Errorf("active-source header exceeds %d bytes", maxEncodedSize)
	}
	return encoded, nil
}

// Decode reads a header value and accepts only the encoding Encode would have produced for the
// same sources: strict base64url, no unknown fields or trailing JSON, a known version, and the
// canonical order. A header that decodes to the right sources in another shape is refused.
func Decode(encoded string) ([]Source, error) {
	if encoded == "" || encoded != strings.TrimSpace(encoded) {
		return nil, errors.New("active-source header is empty or non-canonical")
	}
	if len(encoded) > maxEncodedSize {
		return nil, fmt.Errorf("active-source header exceeds %d bytes", maxEncodedSize)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid active-source encoding: %w", err)
	}
	if len(raw) > maxDecodedSize {
		return nil, fmt.Errorf("active-source payload exceeds %d bytes", maxDecodedSize)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var env envelope
	if err := decoder.Decode(&env); err != nil {
		return nil, fmt.Errorf("invalid active-source payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("active-source payload has trailing JSON")
		}
		return nil, fmt.Errorf("invalid active-source payload: %w", err)
	}
	if env.Version != version {
		return nil, fmt.Errorf("active-source version %d is unsupported", env.Version)
	}
	canonical, err := Encode(env.Sources)
	if err != nil {
		return nil, err
	}
	if canonical != encoded {
		return nil, errors.New("active-source header is not canonical")
	}
	return env.Sources, nil
}

func validate(source Source) error {
	for name, value := range map[string]string{
		"conversation_id": source.ConversationID,
		"turn_id":         source.TurnID,
	} {
		if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maxIDRunes {
			return fmt.Errorf("active-source %s is empty, non-canonical, or over %d runes", name, maxIDRunes)
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return fmt.Errorf("active-source %s contains a control character", name)
			}
		}
	}
	return nil
}
