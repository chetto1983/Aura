package config

import "testing"

// TestPhase13ConfigDefaultsAndOverrides locks the Phase 13 channels/setup/
// multimodal knobs (split out of config_test.go on the 13-04 touch to keep that
// file under the 600-LOC cap). Defaults: AURA_SETUP_BIND on :9081 (a loopback
// port DISTINCT from the AG-UI :9080, the separate-port requirement /
// T-13-04-SetupExposure), AURA_VISION_CLOUD=false (local GLM-OCR),
// AURA_SETUP_TOKEN empty (generated at boot, 13-07), and the sidecar URL/model
// knobs (upstream naming) with their documented fallbacks. Overrides honored.
func TestPhase13ConfigDefaultsAndOverrides(t *testing.T) {
	clearPostgresEnv(t)

	cfg := LoadDB()
	if cfg.SetupBind != "127.0.0.1:9081" {
		t.Errorf("SetupBind default = %q, want 127.0.0.1:9081", cfg.SetupBind)
	}
	// Separate-port requirement: the setup bind must NOT collide with AG-UI's :9080.
	if cfg.SetupBind == cfg.AGUIBind {
		t.Errorf("SetupBind (%q) must differ from AGUIBind (%q)", cfg.SetupBind, cfg.AGUIBind)
	}
	if cfg.SetupToken != "" {
		t.Errorf("SetupToken default = %q, want empty (generated at boot)", cfg.SetupToken)
	}
	if cfg.VisionCloud {
		t.Error("VisionCloud default = true, want false (local GLM-OCR sidecar)")
	}
	if cfg.MultimodalFallbackModel != "minimax/minimax-m3" {
		t.Errorf("MultimodalFallbackModel default = %q, want minimax/minimax-m3", cfg.MultimodalFallbackModel)
	}
	if cfg.TTSVoice != "if_sara" {
		t.Errorf("TTSVoice default = %q, want if_sara", cfg.TTSVoice)
	}
	if cfg.TTSFormat != "opus" {
		t.Errorf("TTSFormat default = %q, want opus", cfg.TTSFormat)
	}
	// URL/model knobs with no default stay empty until the operator wires the sidecars.
	if cfg.MultimodalBaseURL != "" || cfg.STTBaseURL != "" || cfg.TTSBaseURL != "" || cfg.DocumentsBaseURL != "" {
		t.Errorf("sidecar base URLs should default empty, got mm=%q stt=%q tts=%q docs=%q",
			cfg.MultimodalBaseURL, cfg.STTBaseURL, cfg.TTSBaseURL, cfg.DocumentsBaseURL)
	}

	t.Setenv("AURA_SETUP_BIND", "0.0.0.0:9099")
	t.Setenv("AURA_SETUP_TOKEN", "tok-123")
	t.Setenv("AURA_VISION_CLOUD", "true")
	t.Setenv("MULTIMODAL_BASE_URL", "http://aura-ocr-vl:8082/v1")
	t.Setenv("MULTIMODAL_MODEL", "glm-ocr")
	t.Setenv("MULTIMODAL_FALLBACK_MODEL", "openrouter/some-vision")
	t.Setenv("STT_BASE_URL", "http://aura-stt:9000/v1")
	t.Setenv("STT_MODEL", "large-v3-turbo")
	t.Setenv("TTS_BASE_URL", "http://aura-tts:8880/v1")
	t.Setenv("TTS_VOICE", "if_other")
	t.Setenv("TTS_FORMAT", "mp3")
	t.Setenv("DOCUMENTS_BASE_URL", "http://markitdown:5000")

	cfg = LoadDB()
	if cfg.SetupBind != "0.0.0.0:9099" {
		t.Errorf("SetupBind override = %q", cfg.SetupBind)
	}
	if cfg.SetupToken != "tok-123" {
		t.Errorf("SetupToken override = %q", cfg.SetupToken)
	}
	if !cfg.VisionCloud {
		t.Error("VisionCloud override = false, want true")
	}
	if cfg.MultimodalBaseURL != "http://aura-ocr-vl:8082/v1" {
		t.Errorf("MultimodalBaseURL override = %q", cfg.MultimodalBaseURL)
	}
	if cfg.MultimodalModel != "glm-ocr" {
		t.Errorf("MultimodalModel override = %q", cfg.MultimodalModel)
	}
	if cfg.MultimodalFallbackModel != "openrouter/some-vision" {
		t.Errorf("MultimodalFallbackModel override = %q", cfg.MultimodalFallbackModel)
	}
	if cfg.STTBaseURL != "http://aura-stt:9000/v1" || cfg.STTModel != "large-v3-turbo" {
		t.Errorf("STT override = %q / %q", cfg.STTBaseURL, cfg.STTModel)
	}
	if cfg.TTSBaseURL != "http://aura-tts:8880/v1" || cfg.TTSVoice != "if_other" || cfg.TTSFormat != "mp3" {
		t.Errorf("TTS override = %q / %q / %q", cfg.TTSBaseURL, cfg.TTSVoice, cfg.TTSFormat)
	}
	if cfg.DocumentsBaseURL != "http://markitdown:5000" {
		t.Errorf("DocumentsBaseURL override = %q, want http://markitdown:5000", cfg.DocumentsBaseURL)
	}
}

// TestWhatsAppBridgeURLDefaultAndOverride locks the cockpit Connect knob: unset → the
// in-compose sibling default; set → the override. An unset value must NOT be boot-fatal
// (the connect routes answer 503 at call time, the SetWhatsAppBridge precedent).
func TestWhatsAppBridgeURLDefaultAndOverride(t *testing.T) {
	clearPostgresEnv(t)

	if cfg := LoadDB(); cfg.WhatsAppBridgeURL != "http://whatsapp:8081" {
		t.Errorf("WhatsAppBridgeURL default = %q, want http://whatsapp:8081", cfg.WhatsAppBridgeURL)
	}

	t.Setenv("AURA_WHATSAPP_BRIDGE_URL", "http://127.0.0.1:8094")
	if cfg := LoadDB(); cfg.WhatsAppBridgeURL != "http://127.0.0.1:8094" {
		t.Errorf("WhatsAppBridgeURL override = %q, want http://127.0.0.1:8094", cfg.WhatsAppBridgeURL)
	}
}
