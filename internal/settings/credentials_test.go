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
		key, err := creds.PrimaryRouteKey(tc.provider, tc.base)(t.Context())
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

func TestHouseKeyIgnoresTheEnvironment(t *testing.T) {
	env := func(string) (string, bool) { return "sk-ant-env", true }
	if key, err := (Credentials{LookupEnv: env}).AnthropicKey(t.Context()); err != nil || key != "" {
		t.Fatalf("no store: key = %q, %v, want empty", key, err)
	}
	empty := Credentials{Store: &secretRows{values: map[string]string{}}, LookupEnv: env}
	if key, err := empty.AnthropicKey(t.Context()); err != nil || key != "" {
		t.Fatalf("empty row: key = %q, %v, want empty: the environment is never read", key, err)
	}
}

func TestHouseKeyReadsTheRowOnEveryCallAndReturnsTheStoreError(t *testing.T) {
	rows := &secretRows{values: map[string]string{AnthropicKeySetting: " k1 "}}
	creds := Credentials{Store: rows}
	if key, _ := creds.AnthropicKey(t.Context()); key != "k1" {
		t.Fatalf("key = %q, want k1", key)
	}
	rows.values[AnthropicKeySetting] = "k2"
	if key, _ := creds.AnthropicKey(t.Context()); key != "k2" {
		t.Fatalf("key = %q, want the replaced k2", key)
	}
	rows.err = errBoom
	if _, err := creds.AnthropicKey(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want the store error", err)
	}
}

func TestPrimaryRouteKeyStaysEmptyOnTheAnthropicRoute(t *testing.T) {
	creds := Credentials{Store: &secretRows{values: map[string]string{
		ServicesKeySetting: "services", AnthropicKeySetting: "house",
	}}}
	key, err := creds.PrimaryRouteKey(llm.AnthropicProvider, "https://api.anthropic.com")(t.Context())
	if err != nil || key != "" {
		t.Fatalf("key = %q, %v, want empty: vision's HTTP arm is OpenAI-shaped", key, err)
	}
}
