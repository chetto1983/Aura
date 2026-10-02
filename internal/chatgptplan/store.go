package chatgptplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/secret"
	"github.com/google/uuid"
)

type credential struct {
	Owner             string    `json:"owner"`
	Issuer            string    `json:"issuer"`
	Subject           string    `json:"subject"`
	ClientID          string    `json:"client_id"`
	HostID            string    `json:"ext_agent_host_id"`
	Email             string    `json:"email"`
	IDToken           string    `json:"id_token,omitempty"`
	AccessToken       string    `json:"access_token,omitempty"`
	RefreshToken      string    `json:"refresh_token,omitempty"`
	TokenType         string    `json:"token_type,omitempty"`
	Scopes            []string  `json:"scopes,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitzero"`
	EarliestRefreshAt time.Time `json:"earliest_refresh_at,omitzero"`
}

func (s *Service) path(owner string) string {
	digest := sha256.Sum256([]byte(owner))
	return filepath.Join(s.dir, hex.EncodeToString(digest[:])+".enc")
}

func (s *Service) sealer(owner string) (*secret.Sealer, error) {
	return secret.NewSealer(s.masterSecret, "aura-chatgpt-plan-v1\x00"+owner)
}

func (s *Service) load(owner string) (*credential, error) {
	data, err := os.ReadFile(s.path(owner))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("chatgpt plan: cannot read protected credentials")
	}
	sealer, err := s.sealer(owner)
	if err != nil {
		return nil, err
	}
	plain, err := sealer.Open(data)
	if err != nil {
		return nil, errors.New("chatgpt plan: cannot decrypt credentials")
	}
	var c credential
	if err = json.Unmarshal(plain, &c); err != nil || c.Owner != owner || c.Issuer != s.endpoint.issuer || c.HostID != s.hostID || c.ClientID == "" || c.ClientID == dynamicClient || c.Subject == "" {
		return nil, errors.New("chatgpt plan: invalid protected credential record")
	}
	return &c, nil
}

func (s *Service) save(c *credential) error {
	data, err := json.Marshal(c) // #nosec G117 -- The complete token record is AES-GCM sealed before its only write.
	if err != nil {
		return err
	}
	sealer, err := s.sealer(c.Owner)
	if err != nil {
		return err
	}
	sealed, err := sealer.Seal(data)
	if err != nil {
		return err
	}
	return atomicWrite(s.path(c.Owner), sealed)
}

func atomicWrite(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".credential-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	defer func() { _ = f.Close() }()
	if err = privatePath(f.Name(), false); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return privatePath(path, true)
}

func loadHostID(dir string) (string, error) {
	path := filepath.Join(dir, "host-id")
	data, err := os.ReadFile(path) // #nosec G304 -- Fixed host-id filename beneath the application's private directory.
	if err == nil {
		id := strings.TrimSpace(string(data))
		if !validHostID(id) {
			return "", errors.New("chatgpt plan: invalid saved host ID")
		}
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	id := uuid.New().URN()
	return id, atomicWrite(path, []byte(id))
}

func (s *Service) lockStore(ctx context.Context) (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = privatePath(f.Name(), false); err != nil {
		_ = f.Close()
		return nil, err
	}
	for {
		if err = tryFileLock(f); err == nil {
			return func() { _ = unlockFile(f); _ = f.Close() }, nil
		}
		if !lockBusy(err) {
			_ = f.Close()
			return nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
