package agui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

type decodeOpts struct {
	maxBytes   int64
	allowEmpty bool
}

// strictDecodeJSON is the single inbound JSON boundary for privileged routes.
// It preserves the existing one-MiB default, rejects declared non-JSON media
// types and unknown fields, and performs a second decode that must reach EOF so
// concatenated JSON values cannot be smuggled past the first value.
func strictDecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts decodeOpts) error {
	if raw := strings.TrimSpace(r.Header.Get("Content-Type")); raw != "" {
		mediaType, _, err := mime.ParseMediaType(raw)
		if err != nil {
			return fmt.Errorf("invalid Content-Type: %w", err)
		}
		if mediaType != "application/json" {
			return fmt.Errorf("Content-Type must be application/json")
		}
	}

	maxBytes := opts.maxBytes
	if maxBytes <= 0 {
		maxBytes = maxRunBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) && opts.allowEmpty {
			return nil
		}
		return fmt.Errorf("decode JSON body: %w", err)
	}

	var trailing struct{}
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing data after JSON body")
		}
		return fmt.Errorf("unexpected trailing data after JSON body: %w", err)
	}
	return nil
}
