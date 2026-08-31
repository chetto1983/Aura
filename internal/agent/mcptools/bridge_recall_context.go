package mcptools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent/tools"
)

const (
	recallContextHeader         = "X-Aura-Active-Sources"
	recallContextVersion        = 1
	recallContextMaxSources     = 8
	recallContextMaxIDRunes     = 256
	recallContextMaxDecodedSize = 1536
	recallContextMaxEncodedSize = 2048
)

type recallSourceKey struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
}

type recallContextEnvelope struct {
	Version int               `json:"version"`
	Sources []recallSourceKey `json:"sources"`
}

// encodeRecallContextHeader returns canonical unpadded base64url JSON. Sorting
// is part of the wire contract so equivalent source sets have one encoding.
func encodeRecallContextHeader(sources []recallSourceKey) (string, error) {
	if len(sources) == 0 || len(sources) > recallContextMaxSources {
		return "", fmt.Errorf("active recall sources must contain between 1 and %d entries", recallContextMaxSources)
	}
	canonical := append([]recallSourceKey(nil), sources...)
	for _, source := range canonical {
		if err := validateRecallSourceKey(source); err != nil {
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
			return "", fmt.Errorf("active recall source is duplicated")
		}
	}
	raw, err := json.Marshal(recallContextEnvelope{Version: recallContextVersion, Sources: canonical})
	if err != nil {
		return "", fmt.Errorf("encode active recall sources: %w", err)
	}
	if len(raw) > recallContextMaxDecodedSize {
		return "", fmt.Errorf("active recall source payload exceeds %d bytes", recallContextMaxDecodedSize)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > recallContextMaxEncodedSize {
		return "", fmt.Errorf("active recall source header exceeds %d bytes", recallContextMaxEncodedSize)
	}
	return encoded, nil
}

// recallContextHeaderFunc derives active source keys from the current tool call.
// Both values are host-owned: sessionID is the active conversation and requestID
// is the current turn/run identifier. A partial context emits nothing.
func recallContextHeaderFunc(ctx context.Context) map[string]string {
	conversationID := tools.SessionIDFromContext(ctx)
	turnID := tools.RequestIDFromContext(ctx)
	if conversationID == "" || turnID == "" {
		return nil
	}
	encoded, err := encodeRecallContextHeader([]recallSourceKey{{
		ConversationID: conversationID,
		TurnID:         turnID,
	}})
	if err != nil {
		return nil
	}
	return map[string]string{recallContextHeader: encoded}
}

func decodeRecallContextHeader(encoded string) ([]recallSourceKey, error) {
	if encoded == "" || encoded != strings.TrimSpace(encoded) {
		return nil, fmt.Errorf("active recall source header is empty or non-canonical")
	}
	if len(encoded) > recallContextMaxEncodedSize {
		return nil, fmt.Errorf("active recall source header exceeds %d bytes", recallContextMaxEncodedSize)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode active recall source header: %w", err)
	}
	if len(raw) > recallContextMaxDecodedSize {
		return nil, fmt.Errorf("active recall source payload exceeds %d bytes", recallContextMaxDecodedSize)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope recallContextEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode active recall source payload: %w", err)
	}
	if err := ensureRecallContextEOF(decoder); err != nil {
		return nil, err
	}
	if envelope.Version != recallContextVersion {
		return nil, fmt.Errorf("active recall source version %d is unsupported", envelope.Version)
	}
	reencoded, err := encodeRecallContextHeader(envelope.Sources)
	if err != nil {
		return nil, err
	}
	if reencoded != encoded {
		return nil, fmt.Errorf("active recall source header is not canonical")
	}
	return envelope.Sources, nil
}

func validateRecallSourceKey(source recallSourceKey) error {
	for name, value := range map[string]string{
		"conversation_id": source.ConversationID,
		"turn_id":         source.TurnID,
	} {
		if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > recallContextMaxIDRunes {
			return fmt.Errorf("active recall %s is empty, non-canonical, or over %d runes", name, recallContextMaxIDRunes)
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return fmt.Errorf("active recall %s contains a control character", name)
			}
		}
	}
	return nil
}

func ensureRecallContextEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("active recall source payload has trailing JSON")
		}
		return fmt.Errorf("decode active recall source payload: %w", err)
	}
	return nil
}
