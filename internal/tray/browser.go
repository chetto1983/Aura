//go:build windows

package tray

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func validateDashboardURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("dashboard url required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse dashboard url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported dashboard url scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("dashboard url host required")
	}
	return u.String(), nil
}
