package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/settings"
)

// The four live media settings (ruling R3): rows in aura.settings, exactly like
// the primary LLM model. Nothing reads these with os.Getenv.
const (
	imageModelSettingKey         = "AURA_IMAGE_MODEL"
	videoModelSettingKey         = "AURA_VIDEO_MODEL"
	videoInlineWaitSettingKey    = "AURA_VIDEO_INLINE_WAIT_SEC"
	assetMaxVideoBytesSettingKey = "AURA_ASSET_MAX_VIDEO_BYTES"
)

// mediaSettings implements mediagen.Settings over the aura.settings store,
// reading it fresh on every call (ruling R3) — a saved value is live at once,
// with no separate invalidation, exactly like the primary LLM profile's
// call-time keys (internal/agui's callTimeSettingKeys).
type mediaSettings struct {
	lister settings.Lister
}

func newMediaSettings(lister settings.Lister) mediaSettings {
	return mediaSettings{lister: lister}
}

var _ mediagen.Settings = mediaSettings{}

// Model returns the live model for kind: the stored row when present, else the
// matching default. A store error surfaces; it is never swallowed into a
// silent fallback to the default.
func (m mediaSettings) Model(ctx context.Context, kind mediagen.Kind) (string, error) {
	var key, def string
	switch kind {
	case mediagen.KindImage:
		key, def = imageModelSettingKey, mediagen.DefaultImageModel
	case mediagen.KindVideo:
		key, def = videoModelSettingKey, mediagen.DefaultVideoModel
	default:
		return "", fmt.Errorf("mediagen: unknown kind %q", kind)
	}
	value, err := m.storedValue(ctx, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return def, nil
	}
	return value, nil
}

// VideoInlineWait returns the live ceiling a video generation call waits inline
// before detaching to the watcher. Zero is a valid stored value (detach
// immediately); a negative stored value is an error, never silently clamped.
func (m mediaSettings) VideoInlineWait(ctx context.Context) (time.Duration, error) {
	value, err := m.storedValue(ctx, videoInlineWaitSettingKey)
	if err != nil {
		return 0, err
	}
	if value == "" {
		return time.Duration(mediagen.DefaultVideoInlineWaitSec) * time.Second, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("mediagen: %s: %w", videoInlineWaitSettingKey, err)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("mediagen: %s must not be negative, got %d", videoInlineWaitSettingKey, seconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

// storedValue returns the trimmed value of an aura.settings row, "" when the
// row is absent or the lister is unwired.
func (m mediaSettings) storedValue(ctx context.Context, key string) (string, error) {
	if m.lister == nil {
		return "", nil
	}
	rows, err := m.lister.List(ctx)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Key == key {
			return strings.TrimSpace(row.Value), nil
		}
	}
	return "", nil
}

// bootAssetMaxVideoBytes reads AURA_ASSET_MAX_VIDEO_BYTES once at boot (row ->
// default), for assets.Limits.MaxVideoBytes and the media path — unlike the
// other three settings, it is not re-read per call. A stored value no media
// byte limit accepts (mediagen.ValidByteLimit: nonpositive, or math.MaxInt64)
// is a boot error, never a silent fallback to the default, so the asset
// service and the video watcher never disagree about the ceiling.
func bootAssetMaxVideoBytes(ctx context.Context, lister settings.Lister) (int64, error) {
	value, err := (mediaSettings{lister: lister}).storedValue(ctx, assetMaxVideoBytesSettingKey)
	if err != nil {
		return 0, err
	}
	if value == "" {
		return mediagen.DefaultAssetMaxVideoBytes, nil
	}
	maxBytes, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("mediagen: %s: %w", assetMaxVideoBytesSettingKey, err)
	}
	if mediagen.ValidByteLimit(maxBytes) != nil {
		return 0, fmt.Errorf("mediagen: %s must be a positive byte limit below the int64 maximum, got %d",
			assetMaxVideoBytesSettingKey, maxBytes)
	}
	return maxBytes, nil
}
