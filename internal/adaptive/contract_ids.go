package adaptive

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"github.com/google/uuid"
)

var (
	assignmentIDNamespace = uuid.MustParse("c5370396-c73f-4a44-ae7b-112f070523ae")
	eventIDNamespace      = uuid.MustParse("fb3f7ce9-d343-41fb-a26f-35155b229189")
)

func AssignmentIDForPoint(
	ownerID uuid.UUID,
	requestID uuid.UUID,
	point DecisionPoint,
	ordinal uint32,
) uuid.UUID {
	identity := make([]byte, 0, 16+16+1+len(point)+1+4)
	identity = append(identity, ownerID[:]...)
	identity = append(identity, requestID[:]...)
	identity = append(identity, 0)
	identity = append(identity, strings.TrimSpace(string(point))...)
	identity = append(identity, 0)
	identity = binary.BigEndian.AppendUint32(identity, ordinal)
	return uuid.NewHash(sha256.New(), assignmentIDNamespace, identity, 5)
}

func EventIDForSource(
	assignmentID uuid.UUID,
	kind EventKind,
	sourceIdentity string,
) uuid.UUID {
	sourceIdentity = strings.TrimSpace(sourceIdentity)
	identity := make([]byte, 0, len(SchemaVersion2)+1+16+1+len(kind)+1+len(sourceIdentity))
	identity = append(identity, SchemaVersion2...)
	identity = append(identity, 0)
	identity = append(identity, assignmentID[:]...)
	identity = append(identity, 0)
	identity = append(identity, kind...)
	identity = append(identity, 0)
	identity = append(identity, sourceIdentity...)
	return uuid.NewHash(sha256.New(), eventIDNamespace, identity, 5)
}
