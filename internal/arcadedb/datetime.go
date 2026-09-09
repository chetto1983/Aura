package arcadedb

import (
	"fmt"
	"strings"
	"time"
)

// parseArcadeDateTime decodes any ArcadeDB DATETIME property this package reads.
//
// ArcadeDB renders DATETIME with its documented default WITHOUT a zone, even when the
// value written was RFC3339 UTC, so the zone the wire representation omits has to be
// restored. Cypher map projections serialize the same native DATETIME as local ISO,
// omitting seconds when they are zero; SQL row projections use the space form.
// https://docs.arcadedb.com/arcadedb/reference/managing-dates
//
// Every timestamp this package writes is UTC, which is what makes restoring it sound.
func parseArcadeDateTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid arcadedb datetime %q", value)
}
