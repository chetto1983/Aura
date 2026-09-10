package llm

import (
	"net"
	"net/url"
	"strings"
)

// IsKeylessLocalBaseURL reports whether raw points at a backend that bills nothing and takes
// no credential: vLLM, llama.cpp or Ollama on this box or the LAN (D-13).
func IsKeylessLocalBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "host.docker.internal" || strings.HasSuffix(host, ".local") {
		return true
	}
	switch host {
	case "ollama", "vllm", "llama", "llama-cpp", "aura-llm", "aura-vllm-chat", "aura-llama-chat", "aura-llama":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
