package mcptools

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// envMCPCallTimeoutSec bounds an MCP call no agent turn waits on with a window of its
// own: the tool pipe, a notification send. Inside a turn the loop moves a slow call to
// the background and bounds it with its own ceiling instead (tools.CallCeiling).
const envMCPCallTimeoutSec = "AURA_MCP_CALL_TIMEOUT_SEC"

const defaultMCPCallTimeout = 60 * time.Second

func configuredMCPCallTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(envMCPCallTimeoutSec))
	if raw == "" {
		return defaultMCPCallTimeout, nil
	}
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil || sec < 0 {
		return 0, fmt.Errorf("%s=%q: must be 0 for the bounded default or a positive seconds value", envMCPCallTimeoutSec, raw)
	}
	if sec == 0 {
		return defaultMCPCallTimeout, nil
	}
	return time.Duration(sec * float64(time.Second)), nil
}
