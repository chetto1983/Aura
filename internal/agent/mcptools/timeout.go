package mcptools

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const envMCPCallTimeoutSec = "AURA_MCP_CALL_TIMEOUT_SEC"

const defaultMCPCallTimeout = 60 * time.Second

func configuredMCPCallTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(envMCPCallTimeoutSec))
	if raw == "" {
		return defaultMCPCallTimeout, nil
	}
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil || sec < -1 {
		return 0, fmt.Errorf("%s=%q: must be -1 for no timeout, 0 for default, or a positive seconds value", envMCPCallTimeoutSec, raw)
	}
	if sec == -1 {
		return 0, nil
	}
	if sec == 0 {
		return defaultMCPCallTimeout, nil
	}
	return time.Duration(sec * float64(time.Second)), nil
}
