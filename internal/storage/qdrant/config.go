package qdrant

import "time"

// Config holds connection parameters for a Qdrant instance.
type Config struct {
	BaseURL       string
	APIKey        string
	Timeout       time.Duration
	MaxRetryDelay time.Duration // max backoff delay for WaitForReady (default 10s)
}
