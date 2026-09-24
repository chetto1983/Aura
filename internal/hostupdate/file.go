// Package hostupdate is Aura's side of the channel to the appliance's host updater
// (deploy/aura-image-update.sh). The two share one bind-mounted directory and three files,
// each with a single writer: the updater writes status, Aura writes request and activity.
//
// Every file is `key=value` lines because the reader on the host is bash: an appliance is
// guaranteed bash and coreutils, not jq. Writes are a dot-prefixed temporary file renamed
// into place, so a reader never sees half a file and the systemd .path unit watching
// request fires once, on the rename (measured on the lab VM, 2026-09-24).
package hostupdate

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The channel's files, named as deploy/aura-update-consent.sh names them.
const (
	StatusFile   = "status"
	RequestFile  = "request"
	ActivityFile = "activity"
)

var (
	revPattern       = regexp.MustCompile(`^[0-9a-f]{0,64}$`)
	identityPattern  = regexp.MustCompile(`^[0-9a-f-]{0,36}$`)
	requestIDPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	epochPattern     = regexp.MustCompile(`^[0-9]{1,12}$`)
	countPattern     = regexp.MustCompile(`^[0-9]{1,6}$`)
)

// readKeyValues returns nil, false, nil when the file does not exist.
func readKeyValues(dir, name string) (map[string]string, bool, error) {
	body, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: dir is the operator-configured AURA_UPDATE_STATE_DIR and name one of this package's constants.
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, true, fmt.Errorf("hostupdate: %s: line %q is not key=value", name, line)
		}
		values[key] = value
	}
	return values, true, scanner.Err()
}

func writeAtomic(dir, name string, lines []string) error {
	tmp, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { //nolint:gosec // G302: the host updater reads these; they carry ids, epochs and an action, no secret.
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

func parseEpoch(key, value string) (time.Time, error) {
	if value == "" || value == "0" {
		return time.Time{}, nil
	}
	if !epochPattern.MatchString(value) {
		return time.Time{}, fmt.Errorf("hostupdate: %s=%q is not epoch seconds", key, value)
	}
	secs, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(secs, 0).UTC(), nil
}

func formatEpoch(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatInt(t.Unix(), 10)
}

func matched(pattern *regexp.Regexp, key, value string) error {
	if !pattern.MatchString(value) {
		return fmt.Errorf("hostupdate: %s=%q is malformed", key, value)
	}
	return nil
}
