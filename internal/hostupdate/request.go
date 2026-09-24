package hostupdate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// The decisions an admin can send.
const (
	ActionApply = "apply"
	ActionDefer = "defer"
)

// Request is an admin's decision for the updater. A new ID is what makes the updater act:
// it records the last ID it handled and ignores a request it has already seen.
type Request struct {
	ID     string
	Action string
	Until  time.Time
	By     string
}

// NewRequestID returns 32 random hex characters.
func NewRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// WriteRequest validates exactly what the updater's regexes accept, so a request the
// updater would discard is refused here with an error instead of silently ignored there.
func WriteRequest(dir string, r Request) error {
	if err := r.validate(); err != nil {
		return err
	}
	return writeAtomic(dir, RequestFile, []string{
		"id=" + r.ID,
		"action=" + r.Action,
		"until=" + formatEpoch(r.Until),
		"by=" + r.By,
	})
}

// ReadRequest reports false when no request was ever written to dir.
func ReadRequest(dir string) (Request, bool, error) {
	values, ok, err := readKeyValues(dir, RequestFile)
	if err != nil || !ok {
		return Request{}, ok, err
	}
	until, err := parseEpoch("until", values["until"])
	if err != nil {
		return Request{}, true, err
	}
	r := Request{ID: values["id"], Action: values["action"], Until: until, By: values["by"]}
	return r, true, r.validate()
}

func (r Request) validate() error {
	if err := matched(requestIDPattern, "id", r.ID); err != nil {
		return err
	}
	if err := matched(identityPattern, "by", r.By); err != nil {
		return err
	}
	switch r.Action {
	case ActionApply:
		return nil
	case ActionDefer:
		if r.Until.IsZero() {
			return errors.New("hostupdate: a deferral needs an until")
		}
		return nil
	default:
		return fmt.Errorf("hostupdate: unknown action %q", r.Action)
	}
}
