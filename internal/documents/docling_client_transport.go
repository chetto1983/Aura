package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	doclingDialTimeout       = 10 * time.Second
	doclingRequestTimeout    = 2 * time.Minute
	doclingStatusBodyLimit   = 1 << 20
	doclingResultBodyLimit   = 256 << 20
	doclingDefaultPollPeriod = 500 * time.Millisecond
)

type doclingResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type doclingDialError struct{}

func (*doclingDialError) Error() string { return "private Docling target rejected" }

func isAllowedDoclingIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	return (ip.IsLoopback() || ip.IsPrivate()) &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified()
}

func resolvePrivateDoclingIP(ctx context.Context, resolver doclingResolver, host string) (netip.Addr, error) {
	if parsed, err := netip.ParseAddr(host); err == nil {
		if !isAllowedDoclingIP(parsed) {
			return netip.Addr{}, &doclingDialError{}
		}
		return parsed.Unmap(), nil
	}
	ips, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, &doclingDialError{}
	}
	var first netip.Addr
	for _, ip := range ips {
		if !isAllowedDoclingIP(ip) {
			return netip.Addr{}, &doclingDialError{}
		}
		if !first.IsValid() {
			first = ip.Unmap()
		}
	}
	return first, nil
}

func newPrivateDoclingHTTPClient(resolver doclingResolver) *http.Client {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := &net.Dialer{Timeout: doclingDialTimeout}
	transport := &http.Transport{
		Proxy:             nil,
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, &doclingDialError{}
			}
			ip, err := resolvePrivateDoclingIP(ctx, resolver, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func decodeDoclingJSON(resp *http.Response, limit int64, out any) error {
	if resp.ContentLength > limit {
		return &DoclingError{Operation: "decode", Class: DoclingErrorResponseTooLarge}
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &DoclingError{Operation: "decode", Class: DoclingErrorProtocol}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return &DoclingError{Operation: "decode", Class: DoclingErrorUnavailable, Retryable: true}
	}
	if int64(len(raw)) > limit {
		return &DoclingError{Operation: "decode", Class: DoclingErrorResponseTooLarge}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return &DoclingError{Operation: "decode", Class: DoclingErrorProtocol}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &DoclingError{Operation: "decode", Class: DoclingErrorProtocol}
	}
	return nil
}

func doclingHTTPError(operation string, status int) error {
	errorValue := &DoclingError{Operation: operation, HTTPStatus: status}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		errorValue.Class = DoclingErrorAuthentication
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		errorValue.Class = DoclingErrorPolicy
	case http.StatusConflict, http.StatusTooManyRequests:
		errorValue.Class = DoclingErrorCapacity
		errorValue.Retryable = true
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusNotFound:
		errorValue.Class = DoclingErrorUnavailable
		errorValue.Retryable = true
	default:
		if status >= http.StatusInternalServerError {
			errorValue.Class = DoclingErrorUnavailable
			errorValue.Retryable = true
		} else {
			errorValue.Class = DoclingErrorProtocol
		}
	}
	return errorValue
}

func classifyDoclingRequestError(operation string, err error) error {
	var privacyErr *doclingDialError
	if errors.As(err, &privacyErr) {
		return &DoclingError{Operation: operation, Class: DoclingErrorPrivacy}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &DoclingError{Operation: operation, Class: DoclingErrorTimeout, Retryable: true}
	}
	if errors.Is(err, context.Canceled) {
		return &DoclingError{Operation: operation, Class: DoclingErrorTimeout}
	}
	return &DoclingError{Operation: operation, Class: DoclingErrorUnavailable, Retryable: true}
}

func positiveSeconds(duration time.Duration) string {
	seconds := duration.Seconds()
	return strconv.FormatFloat(seconds, 'f', -1, 64)
}

func contentTypeOrDefault(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(value); err != nil {
		return "application/octet-stream"
	}
	return value
}
