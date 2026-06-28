package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type check struct {
	Name    string
	File    string
	Pattern string
	Want    bool
	Why     string
}

func main() {
	root := os.Getenv("AURA_OPTILLM_ROOT")
	if root == "" {
		root = `D:\tmp\aura-spike-040-adaptive-reasoning\optillm`
	}
	log("START", "root=%s", root)

	checks := []check{
		{
			Name:    "proxy-is-openai-compatible",
			File:    "README.md",
			Pattern: "OpenAI API-compatible optimizing inference proxy",
			Want:    true,
			Why:     "OptiLLM itself can sit behind Aura's OpenAI-compatible client.",
		},
		{
			Name:    "autothink-not-proxy-mode",
			File:    "README.md",
			Pattern: "[AutoThink](optillm/autothink)       |  N/A for proxy",
			Want:    true,
			Why:     "The upstream matrix explicitly excludes AutoThink from proxy-only use.",
		},
		{
			Name:    "proxy-image-excludes-transformers",
			File:    "requirements_proxy_only.txt",
			Pattern: "transformers",
			Want:    false,
			Why:     "Proxy-only image cannot run AutoThink's local model processor.",
		},
		{
			Name:    "full-image-includes-transformers",
			File:    "requirements.txt",
			Pattern: "transformers",
			Want:    true,
			Why:     "Full OptiLLM environment has the local model dependencies AutoThink needs.",
		},
		{
			Name:    "full-image-includes-torch",
			File:    "requirements.txt",
			Pattern: "torch",
			Want:    true,
			Why:     "AutoThink generation and steering are PyTorch-bound.",
		},
		{
			Name:    "autothink-direct-transformers-api",
			File:    filepath.Join("optillm", "autothink", "autothink.py"),
			Pattern: "PreTrainedModel",
			Want:    true,
			Why:     "Public AutoThink entrypoint takes local model/tokenizer objects.",
		},
		{
			Name:    "autothink-manual-token-loop",
			File:    filepath.Join("optillm", "autothink", "processor.py"),
			Pattern: "DynamicCache",
			Want:    true,
			Why:     "The implementation manually advances generation with KV cache state.",
		},
		{
			Name:    "autothink-runtime-pip-install",
			File:    filepath.Join("optillm", "autothink", "classifier.py"),
			Pattern: "pip install adaptive-classifier",
			Want:    true,
			Why:     "Aura should not adopt this auto-install behavior in production.",
		},
		{
			Name:    "autothink-forward-hook",
			File:    filepath.Join("optillm", "autothink", "steering.py"),
			Pattern: "register_forward_hook",
			Want:    true,
			Why:     "Steering requires internal module access and target-layer tuning.",
		},
	}

	failures := 0
	for _, c := range checks {
		ok, err := contains(filepath.Join(root, c.File), c.Pattern)
		if err != nil {
			failures++
			log("MISS", "%s: %v", c.Name, err)
			continue
		}
		if ok != c.Want {
			failures++
			log("MISS", "%s: pattern presence=%v want=%v", c.Name, ok, c.Want)
			continue
		}
		log("CHECK", "%s: %s", c.Name, c.Why)
	}

	server, err := os.ReadFile(filepath.Join(root, "optillm", "server.py"))
	if err != nil {
		failures++
		log("MISS", "server-known-approaches: %v", err)
	} else {
		known := extractKnownApproaches(string(server))
		if containsString(known, "autothink") {
			failures++
			log("MISS", "server-known-approaches: autothink unexpectedly appears in known_approaches=%v", known)
		} else {
			log("CHECK", "server-known-approaches: autothink absent from proxy dispatch list %v", known)
		}
	}

	if failures > 0 {
		log("SUMMARY", "PARTIAL: %d runtime-fit assertions failed", failures)
		os.Exit(1)
	}

	log("FINDING", "OptiLLM proxy is generally OpenAI-compatible, but upstream marks AutoThink as not proxy-compatible.")
	log("FINDING", "Full AutoThink needs a local Transformers/PyTorch process and, for steering, layer-specific hooks plus HF vector datasets.")
	log("FINDING", "Aura can mount OptiLLM as a future heavy sidecar only if it accepts Python ML deps and a resident local model; do not route existing OpenRouter/llama.cpp traffic through AutoThink expecting full behavior.")
	log("SUMMARY", "VALIDATED: AutoThink full runtime is not a drop-in Aura OpenAI-compatible proxy feature")
}

func contains(path string, pattern string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(b), pattern), nil
}

func extractKnownApproaches(src string) []string {
	re := regexp.MustCompile(`known_approaches\s*=\s*\[([^\]]+)\]`)
	m := re.FindStringSubmatch(src)
	if len(m) != 2 {
		return nil
	}
	raw := strings.Split(m[1], ",")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, `"'`)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func log(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}
