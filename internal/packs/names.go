package packs

import (
	"fmt"
	"regexp"
	"strings"
)

// nameShape is Agent Plugins 1.0 §5.5: 1-64 characters of a-z, 0-9, '-' and '.', starting and
// ending alphanumeric. The ban on "--" and ".." is checked beside it.
var nameShape = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$`)

// ValidPluginName applies the Agent Plugins name rule. Aura applies it to both formats at
// install, because the name becomes a host directory and an in-box path.
func ValidPluginName(name string) error {
	return validName("plugin name", name)
}

// ValidConnectorName applies the same rule to a connector. Its name is a registry key, the
// prefix of every tool the server mounts, and a line of the approval card: the character set
// leaves no room for the "__" tool separator or for a newline.
func ValidConnectorName(name string) error {
	return validName("connector name", name)
}

func validName(kind, name string) error {
	if nameShape.MatchString(name) && !strings.Contains(name, "--") && !strings.Contains(name, "..") {
		return nil
	}
	return fmt.Errorf("%s %q: want 1-64 characters of a-z, 0-9, '-' and '.', starting and ending with a letter or digit, without '--' or '..'", kind, name)
}

// NormalizeTransport maps the transport a plugin declares onto the type Aura's registry
// classifies. Agent Plugins writes "streamable-http", the Claude format writes "http", and
// mcp.Classify knows only "stdio" and "streamable_http". Legacy HTTP+SSE has no client in
// Aura, so it is unsupported rather than silently dialled as something else (§7.2.1:
// the declared transport is the one used).
func NormalizeTransport(t string) (string, error) {
	switch strings.TrimSpace(t) {
	case "", "stdio":
		return "stdio", nil
	case "streamable-http", "http", "streamable_http":
		return "streamable_http", nil
	default:
		return "", fmt.Errorf("unsupported transport %q", t)
	}
}
