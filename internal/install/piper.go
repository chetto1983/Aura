package install

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// Pinned upstream coordinates for the Piper Italian Paola medium voice.
// Both the ONNX model and its JSON sidecar config must be present for Piper
// to load the voice.
//
// The SHA-256 values below were verified 2026-05-19:
//   - ONNX: HF LFS metadata sha256 field for the resolve URL
//   - JSON: local download + sha256sum (not an LFS file; inline in repo)
// Rotation procedure:
//  1. curl -sL --max-time 600 <URL> -o /tmp/<filename>
//  2. sha256sum /tmp/<filename>
//  3. paste lowercase hex into the corresponding const + bump the date comment
const (
	PiperVoiceFilename       = "it_IT-paola-medium.onnx"
	PiperVoiceConfigFilename = "it_IT-paola-medium.onnx.json"

	piperVoiceBaseURL = "https://huggingface.co/rhasspy/piper-voices/resolve/main/it/it_IT/paola/medium/"

	PiperVoiceURL       = piperVoiceBaseURL + PiperVoiceFilename
	PiperVoiceConfigURL = piperVoiceBaseURL + PiperVoiceConfigFilename

	PiperVoiceSHA256       = "6fc918b5a0ea6137382833dddfa567bffbe6a5060c02043c87192ee59c04210c"
	PiperVoiceConfigSHA256 = "aea19c0a7fce29fbc359b93f10e7902854401e4c95ae2ea328ae516b15d296cf"

	PiperVoiceSize       = 63_511_038
	PiperVoiceConfigSize = 7_099
)

// Env vars allow operators to point at a private mirror without recompiling.
const (
	envPiperVoiceURL       = "AURA_PIPER_VOICE_URL"
	envPiperVoiceSHA256    = "AURA_PIPER_VOICE_SHA256"
	envPiperVoiceCfgURL    = "AURA_PIPER_VOICE_CFG_URL"
	envPiperVoiceCfgSHA256 = "AURA_PIPER_VOICE_CFG_SHA256"
)

// EnsurePiperVoice guarantees both the Piper ONNX model and its JSON config
// exist under dataDir/piper/ with verified SHA-256 hashes. Creating the piper/
// subdir matches the compose mount: ./data/piper:/voices:ro in aura-piper.
func EnsurePiperVoice(ctx context.Context, dataDir string, progress ProgressFn) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("install: dataDir required")
	}
	piperDir := filepath.Join(dataDir, "piper")

	onnxTarget := filepath.Join(piperDir, PiperVoiceFilename)
	if err := EnsureParentDir(onnxTarget); err != nil {
		return err
	}
	onnxURL := envOrDefault(envPiperVoiceURL, PiperVoiceURL)
	onnxSHA := envOrDefault(envPiperVoiceSHA256, PiperVoiceSHA256)
	if err := Download(ctx, DownloadOpts{
		URL:      onnxURL,
		Dest:     onnxTarget,
		SHA256:   onnxSHA,
		Progress: progress,
	}); err != nil {
		return err
	}

	cfgTarget := filepath.Join(piperDir, PiperVoiceConfigFilename)
	if err := EnsureParentDir(cfgTarget); err != nil {
		return err
	}
	cfgURL := envOrDefault(envPiperVoiceCfgURL, PiperVoiceConfigURL)
	cfgSHA := envOrDefault(envPiperVoiceCfgSHA256, PiperVoiceConfigSHA256)
	return Download(ctx, DownloadOpts{
		URL:      cfgURL,
		Dest:     cfgTarget,
		SHA256:   cfgSHA,
		Progress: progress,
	})
}
