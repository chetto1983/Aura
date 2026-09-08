package config

import (
	"reflect"
	"testing"
)

func TestArtifactConnectOrigins(t *testing.T) {
	t.Setenv("AURA_ARTIFACT_CONNECT_ORIGINS", "")
	if origins := LoadDB().ArtifactConnectOrigins; len(origins) != 0 {
		t.Fatalf("default must be offline: %v", origins)
	}
	t.Setenv("AURA_ARTIFACT_CONNECT_ORIGINS", "https://api.example.test, https://weather.example.test")
	want := []string{"https://api.example.test", "https://weather.example.test"}
	if origins := LoadDB().ArtifactConnectOrigins; !reflect.DeepEqual(origins, want) {
		t.Fatalf("origins = %v, want %v", origins, want)
	}
}
