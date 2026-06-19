package assets

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type VisionConfig struct {
	VisionCloud       bool
	Model             string
	MultimodalBaseURL string
	MultimodalModel   string
	FallbackModel     string
	OpenRouterBaseURL string
	OpenRouterAPIKey  string
	TimeoutSec        int
}

type STTConfig struct {
	BaseURL    string
	Model      string
	Language   string
	TimeoutSec int
}

const (
	defaultSidecarTimeoutSec = 30
	connectTimeoutSec        = 10
)

func sidecarHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: connectTimeoutSec * time.Second,
			}).DialContext,
			DisableKeepAlives: true,
		},
	}
}

func timeoutContext(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = defaultSidecarTimeoutSec
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}

type sidecarStatusError struct {
	endpoint   string
	statusCode int
}

func (e *sidecarStatusError) Error() string {
	return fmt.Sprintf("asset sidecar %s: HTTP %d", e.endpoint, e.statusCode)
}
