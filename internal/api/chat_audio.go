package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ChatVoiceService synthesizes a web chat reply into playable audio.
type ChatVoiceService interface {
	Synthesize(ctx context.Context, text string) (ChatAudio, error)
}

// ChatAudio is a synthesized audio artifact held in the in-memory web cache.
type ChatAudio struct {
	Bytes       []byte
	ContentType string
}

// AudioCache stores short-lived synthesized web audio by opaque ID.
type AudioCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	maxBytes int64
	total    int64
	now      func() time.Time
	entries  map[string]audioEntry
}

type audioEntry struct {
	audio     ChatAudio
	size      int64
	createdAt time.Time
	expiresAt time.Time
}

// NewAudioCache creates a bounded in-memory cache. Deleting it loses only
// cached audio, never canonical chat state.
func NewAudioCache(ttl time.Duration, maxBytes int64) *AudioCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if maxBytes <= 0 {
		maxBytes = 100 * 1024 * 1024
	}
	return &AudioCache{
		ttl:      ttl,
		maxBytes: maxBytes,
		now:      time.Now,
		entries:  make(map[string]audioEntry),
	}
}

func (c *AudioCache) Store(audio ChatAudio) (string, error) {
	if c == nil {
		return "", errors.New("api: audio cache unavailable")
	}
	audio.ContentType = strings.TrimSpace(audio.ContentType)
	if audio.ContentType == "" {
		audio.ContentType = "audio/ogg"
	}
	size := int64(len(audio.Bytes))
	if size == 0 {
		return "", errors.New("api: empty audio")
	}
	if size > c.maxBytes {
		return "", errors.New("api: audio exceeds cache cap")
	}
	id, err := randomAudioID()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().UTC()
	c.evictExpiredLocked(now)
	for c.total+size > c.maxBytes {
		if !c.evictOldestLocked() {
			break
		}
	}
	c.entries[id] = audioEntry{
		audio:     ChatAudio{Bytes: append([]byte(nil), audio.Bytes...), ContentType: audio.ContentType},
		size:      size,
		createdAt: now,
		expiresAt: now.Add(c.ttl),
	}
	c.total += size
	return id, nil
}

func (c *AudioCache) Lookup(id string) (ChatAudio, bool) {
	if c == nil {
		return ChatAudio{}, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ChatAudio{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().UTC()
	entry, ok := c.entries[id]
	if !ok {
		return ChatAudio{}, false
	}
	if !entry.expiresAt.After(now) {
		c.deleteLocked(id, entry)
		return ChatAudio{}, false
	}
	return ChatAudio{Bytes: append([]byte(nil), entry.audio.Bytes...), ContentType: entry.audio.ContentType}, true
}

func (c *AudioCache) evictExpiredLocked(now time.Time) {
	for id, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			c.deleteLocked(id, entry)
		}
	}
}

func (c *AudioCache) evictOldestLocked() bool {
	var oldestID string
	var oldest audioEntry
	for id, entry := range c.entries {
		if oldestID == "" || entry.createdAt.Before(oldest.createdAt) {
			oldestID = id
			oldest = entry
		}
	}
	if oldestID == "" {
		return false
	}
	c.deleteLocked(oldestID, oldest)
	return true
}

func (c *AudioCache) deleteLocked(id string, entry audioEntry) {
	delete(c.entries, id)
	c.total -= entry.size
	if c.total < 0 {
		c.total = 0
	}
}

func randomAudioID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "aud_" + hex.EncodeToString(raw[:]), nil
}

func maybeAttachVoice(r *http.Request, deps Deps, ctx context.Context, reply ChatReply) ChatReply {
	if !chatVoiceRequested(r) || deps.ChatVoice == nil || deps.ChatAudio == nil || strings.TrimSpace(reply.Reply) == "" {
		return reply
	}
	audio, err := deps.ChatVoice.Synthesize(ctx, reply.Reply)
	if err != nil {
		if deps.Logger != nil {
			deps.Logger.Warn("api: web chat voice synthesis failed", "error", err)
		}
		return reply
	}
	id, err := deps.ChatAudio.Store(audio)
	if err != nil {
		if deps.Logger != nil {
			deps.Logger.Warn("api: web chat audio cache failed", "error", err)
		}
		return reply
	}
	reply.AudioURL = "/api/chat/audio/" + id
	return reply
}

func chatVoiceRequested(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("voice"))) {
	case "all", "on", "true", "1":
		return true
	default:
		return false
	}
}

func handleChatAudio(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.ChatAudio == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "audio not found")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		audio, ok := deps.ChatAudio.Lookup(id)
		if !ok {
			writeError(w, deps.Logger, http.StatusNotFound, "audio not found")
			return
		}
		w.Header().Set("Content-Type", audio.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(audio.Bytes)
	}
}
