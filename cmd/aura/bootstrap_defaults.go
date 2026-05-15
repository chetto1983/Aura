package main

import (
	"os"
	"strconv"
)

const (
	// defaultDBPath is the hardcoded default for local desktop installs.
	defaultDBPath = "./aura.db"
	// defaultContainerDBPath is the hardcoded default for headless/container installs.
	// Containers mount a data volume at ./data so aura.db lives there.
	defaultContainerDBPath = "./data/aura.db"
	// defaultHTTPPort is the hardcoded default for the dashboard HTTP server.
	defaultHTTPPort = "127.0.0.1:8080"
)

// applyBootstrapMetaConfig fills in bootstrap meta-config env vars before
// config.Load(). Called in main() after loadDotEnv so any explicit .env value
// still wins. AURA_TIMEZONE is intentionally left untouched: empty means
// "use OS local timezone" which is handled by config.Location().
func applyBootstrapMetaConfig() {
	setBootstrapMetaConfig(detectHeadless)
}

// setBootstrapMetaConfig applies bootstrap defaults using the given headless
// detector. headlessDetect is injectable so tests can simulate container or
// desktop environments without a real TTY.
func setBootstrapMetaConfig(headlessDetect func() bool) {
	headless := false
	if v := os.Getenv("AURA_HEADLESS"); v == "" {
		headless = headlessDetect()
		val := "false"
		if headless {
			val = "true"
		}
		os.Setenv("AURA_HEADLESS", val)
	} else {
		b, err := strconv.ParseBool(v)
		if err == nil {
			headless = b
		}
	}

	if os.Getenv("DB_PATH") == "" {
		if headless {
			os.Setenv("DB_PATH", defaultContainerDBPath)
		} else {
			os.Setenv("DB_PATH", defaultDBPath)
		}
	}

	if os.Getenv("HTTP_PORT") == "" {
		os.Setenv("HTTP_PORT", defaultHTTPPort)
	}
}

// detectHeadless returns true when stdin is not a character device, which is
// the standard heuristic for container or non-interactive service deployments.
func detectHeadless() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true // non-interactive: assume headless
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}
