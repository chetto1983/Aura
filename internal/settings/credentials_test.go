package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// secretRows is a store whose sealed rows the test can change between reads.
type secretRows struct {
	values map[string]string
	err    error
}

func (s *secretRows) Secret(_ context.Context, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.values[key], nil
}

var errBoom = errors.New("settings store unreachable")

func TestServicesKeyReadsTheRowOnEveryCall(t *testing.T) {
	rows := &secretRows{values: map[string]string{ServicesKeySetting: "k1"}}
	creds := Credentials{Store: rows}
	first, err := creds.ServicesKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rows.values[ServicesKeySetting] = "k2"
	second, err := creds.ServicesKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first != "k1" || second != "k2" {
		t.Fatalf("keys = %q, %q, want the row as it stands at each read", first, second)
	}
}

func TestServicesKeyFallsBackToTheEnvironmentForAnEmptyRow(t *testing.T) {
	creds := Credentials{Store: &secretRows{}, LookupEnv: env(map[string]string{ServicesKeySetting: "env-key"})}
	key, err := creds.ServicesKey(t.Context())
	if err != nil || key != "env-key" {
		t.Fatalf("key = %q, %v, want the environment's", key, err)
	}
}

// A database outage is not a missing key: the cockpit would name the wrong cause.
func TestServicesKeyReturnsTheStoreError(t *testing.T) {
	creds := Credentials{Store: &secretRows{err: errBoom}, LookupEnv: env(map[string]string{ServicesKeySetting: "env-key"})}
	key, err := creds.ServicesKey(t.Context())
	if !errors.Is(err, errBoom) || key != "" {
		t.Fatalf("key = %q, err = %v, want the store's error and no key", key, err)
	}
}

func TestEmbedKeyByRoute(t *testing.T) {
	both := map[string]string{ServicesKeySetting: "services", EmbedEndpointKeySetting: "endpoint"}
	for _, tc := range []struct {
		name  string
		embed config.EmbedConfig
		rows  map[string]string
		want  string
	}{
		{"local sidecar", config.EmbedConfig{BaseURL: "http://aura-llama-embed:8081"}, both, ""},
		{"OpenRouter", config.EmbedConfig{CloudModel: "perplexity/pplx-embed-v1-4b"}, both, "services"},
		{"manual endpoint", config.EmbedConfig{CloudModel: "mistral-embed", CloudBaseURL: "https://api.mistral.ai"}, both, "endpoint"},
		{"manual endpoint without its key", config.EmbedConfig{CloudModel: "mistral-embed", CloudBaseURL: "https://api.mistral.ai"},
			map[string]string{ServicesKeySetting: "services"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, err := Credentials{Store: &secretRows{values: tc.rows}}.EmbedKey(t.Context(), tc.embed)
			if err != nil || key != tc.want {
				t.Fatalf("key = %q, %v, want %q", key, err, tc.want)
			}
		})
	}
}

func TestPrimaryRouteKeyGoesOnlyToOpenRouter(t *testing.T) {
	creds := Credentials{Store: &secretRows{values: map[string]string{ServicesKeySetting: "services"}}}
	for _, tc := range []struct {
		provider, base, want string
	}{
		{"openrouter", llm.DefaultBaseURL, "services"},
		{"ollama", "http://ollama:11434/v1", ""},
		{"chatgpt", "https://api.openai.com/v1", ""},
	} {
		key, err := creds.PrimaryRouteKey(t.Context(), tc.provider, tc.base)
		if err != nil || key != tc.want {
			t.Fatalf("%s at %s: key = %q, %v, want %q", tc.provider, tc.base, key, err, tc.want)
		}
	}
}

func TestEmbedCloudAPIKeyIsSealed(t *testing.T) {
	if !AllowedKeys[EmbedEndpointKeySetting].Secret {
		t.Fatalf("%s must be stored sealed", EmbedEndpointKeySetting)
	}
}
