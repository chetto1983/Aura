package qdrant

import "time"

// Config holds connection parameters for a Qdrant instance.
type Config struct {
	BaseURL       string
	APIKey        string
	Timeout       time.Duration
	MaxRetryDelay time.Duration // max backoff delay for WaitForReady (default 10s)
}

// DefaultConfig returns a Config with sane defaults.
// Caller sets BaseURL and APIKey from environment.
func DefaultConfig() Config {
	return Config{
		Timeout:       30 * time.Second,
		MaxRetryDelay: 10 * time.Second,
	}
}
