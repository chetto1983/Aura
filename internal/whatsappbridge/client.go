// Package whatsappbridge reaches the management REST of the aura-whatsapp tenant gateway
// (chetto1983/whatsapp-mcp, container :8081): the API beside the MCP server that pairs a
// device and reports its state. Every request carries the private bridge bearer and the
// identity as X-Tenant-ID, so the gateway answers from that tenant's own WhatsMeow session
// and never from a caller-supplied selector.
package whatsappbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned when the endpoint or the bearer is unset: a stack without
// the sidecar boots, and every caller answers as if WhatsApp were absent.
var ErrNotConfigured = errors.New("whatsapp bridge not configured")

// ErrNotLinked is returned when the identity has no paired WhatsApp account.
var ErrNotLinked = errors.New("no WhatsApp account is linked for this identity")

// requestTimeout bounds one round-trip to the sibling container, which answers in well
// under a second; it keeps a hung sidecar from stalling a cockpit poll or a scheduler tick.
const requestTimeout = 8 * time.Second

// statusBodyLimit caps the status payload read: a handful of fields.
const statusBodyLimit = 16 << 10

var httpClient = &http.Client{Timeout: requestTimeout}

// Client is the gateway endpoint plus its private bearer. The zero value is unconfigured.
type Client struct {
	baseURL string
	token   string
}

// New builds a client over the gateway base URL and the bridge bearer.
func New(baseURL, token string) Client {
	return Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
	}
}

// Do sends method+path on behalf of identityID. The caller closes the response body.
func (c Client) Do(ctx context.Context, method, path, identityID string) (*http.Response, error) {
	if c.baseURL == "" || c.token == "" {
		return nil, ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Tenant-ID", identityID)
	return httpClient.Do(req)
}

// LinkedNumber returns the phone number of the WhatsApp account identityID paired in the
// cockpit, in the form the MCP send_message takes: country code, no '+'. The MCP server
// has no tool that names its own account, so the pairing status is the source.
func (c Client) LinkedNumber(ctx context.Context, identityID string) (string, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/api/status", identityID)
	if err != nil {
		return "", fmt.Errorf("whatsapp bridge status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whatsapp bridge status: HTTP %d", resp.StatusCode)
	}
	var status struct {
		Paired bool   `json:"paired"`
		JID    string `json:"jid"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, statusBodyLimit)).Decode(&status); err != nil {
		return "", fmt.Errorf("whatsapp bridge status: %w", err)
	}
	if !status.Paired {
		return "", ErrNotLinked
	}
	return phoneNumber(status.JID)
}

// phoneNumber reduces the paired device JID ("393331112222:32@s.whatsapp.net") to its
// number. The ":32" is this linked device; the account is the part before it.
func phoneNumber(jid string) (string, error) {
	user, server, ok := strings.Cut(strings.TrimSpace(jid), "@")
	user, _, _ = strings.Cut(user, ":")
	if !ok || server != "s.whatsapp.net" || user == "" {
		return "", errors.New("whatsapp bridge status: the paired account is not a phone-number JID")
	}
	return user, nil
}
