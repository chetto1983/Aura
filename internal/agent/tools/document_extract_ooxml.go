package tools

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
)

// OOXML limits protect the Aura process after the workspace's compressed-file cap. Office ZIP
// members are attacker-controlled and can expand far beyond their compressed size before the
// existing row/column rendering limits run.
const (
	maxOOXMLMemberBytes  int64 = 16 << 20
	maxOOXMLPackageBytes int64 = 32 << 20
	maxOOXMLTextBytes          = 8 << 20
	maxOOXMLOutputBytes        = 8 << 20
	maxOOXMLDepth              = 128
	maxOOXMLNodes              = 250_000
)

var errOOXMLLimit = errors.New("OOXML extraction limit exceeded")

func ooxmlLimitErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errOOXMLLimit, fmt.Sprintf(format, args...))
}

// ooxmlBudget is shared by every member read from one package. A per-member limit alone still
// permits a workbook with many individually-small sheets to exhaust the process.
type ooxmlBudget struct {
	remaining int64
}

func newOOXMLBudget() *ooxmlBudget {
	return &ooxmlBudget{remaining: maxOOXMLPackageBytes}
}

func (b *ooxmlBudget) readMember(zr *zip.Reader, name string) ([]byte, error) {
	var entry *zip.File
	for _, candidate := range zr.File {
		if candidate.Name == name {
			entry = candidate
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("missing %s", name)
	}
	if entry.UncompressedSize64 > uint64(maxOOXMLMemberBytes) {
		return nil, ooxmlLimitErr("%s declares %d decompressed bytes; member limit is %d",
			name, entry.UncompressedSize64, maxOOXMLMemberBytes)
	}
	if entry.UncompressedSize64 > uint64(b.remaining) {
		return nil, ooxmlLimitErr("%s would exceed the %d-byte package budget",
			name, maxOOXMLPackageBytes)
	}

	body, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", name, err)
	}
	defer func() { _ = body.Close() }()

	limit := min(maxOOXMLMemberBytes, b.remaining)
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, ooxmlLimitErr("%s exceeds the remaining %d-byte decompression budget", name, limit)
	}
	b.remaining -= int64(len(data))
	return data, nil
}

func addOOXMLOutput(used *int, bytes int) error {
	if bytes < 0 || *used > maxOOXMLOutputBytes-bytes {
		return ooxmlLimitErr("rendered text exceeds the %d-byte output limit", maxOOXMLOutputBytes)
	}
	*used += bytes
	return nil
}
