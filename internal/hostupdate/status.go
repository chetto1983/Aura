package hostupdate

import (
	"errors"
	"fmt"
	"time"
)

// The states the updater records.
const (
	StateCurrent  = "current"
	StatePending  = "pending"
	StateApplying = "applying"
	StateFailed   = "failed"
)

// Status is what the host updater last decided. Absent times are zero.
type Status struct {
	State          string
	RunningRev     string
	AvailableRev   string
	AvailableBuilt time.Time
	PendingSince   time.Time
	Deadline       time.Time
	DeferredUntil  time.Time
	DeferredBy     string
	HandledRequest string
	Error          string
	CheckedAt      time.Time
}

// ReadStatus reports managed=false when the updater has never written a status here: a
// development stack, or an appliance whose updater predates this channel. Keys it does not
// know are ignored so an updater one release ahead of the running Aura still reads.
func ReadStatus(dir string) (Status, bool, error) {
	values, ok, err := readKeyValues(dir, StatusFile)
	if err != nil || !ok {
		return Status{}, ok, err
	}
	var s Status
	switch state := values["state"]; state {
	case StateCurrent, StatePending, StateApplying, StateFailed:
		s.State = state
	case "":
		return Status{}, true, errors.New("hostupdate: status carries no state")
	default:
		return Status{}, true, fmt.Errorf("hostupdate: unknown state %q", state)
	}
	s.Error = values["error"]
	for key, dst := range map[string]*string{
		"running_rev":     &s.RunningRev,
		"available_rev":   &s.AvailableRev,
		"handled_request": &s.HandledRequest,
	} {
		if err := matched(revPattern, key, values[key]); err != nil {
			return Status{}, true, err
		}
		*dst = values[key]
	}
	if err := matched(identityPattern, "deferred_by", values["deferred_by"]); err != nil {
		return Status{}, true, err
	}
	s.DeferredBy = values["deferred_by"]
	for key, dst := range map[string]*time.Time{
		"available_built": &s.AvailableBuilt,
		"pending_since":   &s.PendingSince,
		"deadline":        &s.Deadline,
		"deferred_until":  &s.DeferredUntil,
		"checked_at":      &s.CheckedAt,
	} {
		t, err := parseEpoch(key, values[key])
		if err != nil {
			return Status{}, true, err
		}
		*dst = t
	}
	return s, true, nil
}
