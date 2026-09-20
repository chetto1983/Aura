package cloudflareapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BaseURL is Cloudflare's production v4 control-plane endpoint.
const BaseURL = "https://api.cloudflare.com/client/v4"
const responseLimit = 2 << 20

// Client retains its API credential privately and never retries writes automatically.
type Client struct {
	base  string
	token Secret
	http  *http.Client
}

// Format prevents diagnostic formatting from exposing credentials.
func (Client) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "cloudflareapi.Client{[REDACTED]}")
}

// New copies the supplied HTTP client and enforces timeout and redirect boundaries.
func New(baseURL, token string, transport *http.Client) *Client {
	if baseURL == "" {
		baseURL = BaseURL
	}
	h := http.Client{}
	if transport != nil {
		h = *transport
	}
	h.Timeout = 15 * time.Second
	// Never forward the credential or a mutation to a redirect target.
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(baseURL, "/"), token: Secret(token), http: &h}
}

// APIError retains machine-readable classification without upstream message text.
type APIError struct {
	Status    int
	Code      int
	Retryable bool
	reason    string
}

// Error is safe to persist or display without inspecting the upstream response.
func (e *APIError) Error() string {
	return fmt.Sprintf("cloudflare: %s (HTTP %d, code %d)", e.reason, e.Status, e.Code)
}

// Retryable admits only rate limits, server failures and transport timeouts.
func Retryable(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Retryable
}

func failure(reason string) error { return &APIError{reason: reason} }

type pageInfo struct {
	Page       int  `json:"page"`
	PerPage    int  `json:"per_page"`
	TotalCount *int `json:"total_count"`
	TotalPages *int `json:"total_pages"`
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body, result any) (pageInfo, error) {
	var info pageInfo
	base, err := url.Parse(c.base)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && base.Scheme != "http") {
		return info, failure("invalid API URL")
	}
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return info, failure("invalid request")
		}
	}
	u := c.base + path
	if len(query) != 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(encoded))
	if err != nil {
		return info, failure("invalid request")
	}
	if strings.TrimSpace(c.token.Reveal()) == "" {
		return info, failure("API token required")
	}
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		var timeout net.Error
		return info, &APIError{reason: "transport failure", Retryable: errors.As(err, &timeout) && timeout.Timeout() && !errors.Is(err, context.Canceled)}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	fail := &APIError{Status: resp.StatusCode, reason: "request failed", Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode <= 599}
	if err != nil || len(data) > responseLimit {
		var timeout net.Error
		fail.Retryable = fail.Retryable || errors.As(err, &timeout) && timeout.Timeout()
		fail.reason = "unreadable or oversized response"
		return info, fail
	}
	var envelope struct {
		Success *bool `json:"success"`
		Errors  []struct {
			Code int `json:"code"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
		Info   pageInfo        `json:"result_info"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		fail.reason = "invalid response envelope"
		return info, fail
	}
	if len(envelope.Errors) > 0 {
		fail.Code = envelope.Errors[0].Code
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Success == nil || !*envelope.Success || len(envelope.Errors) > 0 {
		return info, fail
	}
	if result != nil {
		if len(envelope.Result) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) || json.Unmarshal(envelope.Result, result) != nil {
			fail.reason = "invalid result"
			return info, fail
		}
	}
	return envelope.Info, nil
}

func list[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	if query == nil {
		query = url.Values{}
	}
	all := []T{}
	for page := 1; page <= 1000; page++ {
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "50")
		var batch []T
		info, err := c.request(ctx, http.MethodGet, path, query, nil, &batch)
		if err != nil {
			return nil, err
		}
		if info.Page != 0 && info.Page != page {
			return nil, failure("inconsistent pagination")
		}
		all = append(all, batch...)
		if info.TotalPages != nil {
			if page >= *info.TotalPages {
				return all, nil
			}
		} else if info.TotalCount != nil {
			if len(all) >= *info.TotalCount {
				return all, nil
			}
		} else {
			if info.Page == 0 && info.PerPage == 0 {
				return all, nil
			}
			perPage := info.PerPage
			if perPage <= 0 {
				perPage = 50
			}
			if len(batch) < perPage {
				return all, nil
			}
		}
		if len(batch) == 0 {
			return nil, failure("inconsistent pagination")
		}
	}
	return nil, failure("pagination limit exceeded")
}

func resourcePath(parts ...string) (string, error) {
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\?#%\r\n\t ") {
			return "", failure("invalid resource identifier")
		}
	}
	return "/" + strings.Join(parts, "/"), nil
}

func get[T any](ctx context.Context, c *Client, method string, body any, parts ...string) (T, error) {
	var result T
	path, err := resourcePath(parts...)
	if err != nil {
		return result, err
	}
	_, err = c.request(ctx, method, path, nil, body, &result)
	return result, err
}

func (c *Client) remove(ctx context.Context, parts ...string) error {
	path, err := resourcePath(parts...)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, http.MethodDelete, path, nil, nil, nil)
	return err
}

func listResource[T any](ctx context.Context, c *Client, query url.Values, parts ...string) ([]T, error) {
	path, err := resourcePath(parts...)
	if err != nil {
		return nil, err
	}
	return list[T](ctx, c, path, query)
}
