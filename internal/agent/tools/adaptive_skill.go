package tools

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

// adaptivePointOrdinal derives a stable per-call ordinal from the request and
// tool-call IDs, so an assignment is reproducible for the same point without the
// query text ever crossing the adaptive port.
func adaptivePointOrdinal(ctx context.Context) uint32 {
	sum := sha256.Sum256([]byte(
		RequestIDFromContext(ctx) + "\x00" + ToolCallIDFromContext(ctx),
	))
	return binary.BigEndian.Uint32(sum[:4])
}

// Skill-routing actions are the frozen strategy IDs accepted by the typed
// adaptive adapter. Static preserves the existing queried-list BM25 order.
const (
	SkillRoutingCatalog = "catalog"
	SkillRoutingStatic  = "static"
)

// SkillRoutingInput is the privacy-safe projection of one nonblank queried
// skill-list point. Candidate IDs are registered skill names; query bytes and
// query-derived fingerprints never cross the adaptive port.
type SkillRoutingInput struct {
	OwnerID         string
	RequestID       string
	PointOrdinal    uint32
	QueryLength     int
	CandidateIDs    []string
	CatalogRevision string
}

// SkillRoutingExecutor orders the already-frozen candidates with one
// registered read-only strategy.
type SkillRoutingExecutor func(context.Context, string) ([]string, error)

// SkillRoutingControl owns schema-2 assignment and delivery persistence for
// queried list calls.
type SkillRoutingControl interface {
	OrderSkillRouting(
		context.Context,
		SkillRoutingInput,
		SkillRoutingExecutor,
	) ([]string, error)
}

type frozenSkillCatalog struct {
	skills          []SkillMeta
	byName          map[string]SkillMeta
	ids             []string
	catalogRevision string
}

func freezeSkills(skills []SkillMeta) frozenSkillCatalog {
	frozen := frozenSkillCatalog{
		skills: slices.Clone(skills),
		byName: make(map[string]SkillMeta, len(skills)),
		ids:    make([]string, 0, len(skills)),
	}
	for _, skill := range frozen.skills {
		frozen.byName[skill.Name] = skill
		frozen.ids = append(frozen.ids, skill.Name)
	}
	payload, _ := json.Marshal(frozen.ids)
	sum := sha256.Sum256(payload)
	frozen.catalogRevision = "skill-catalog-" +
		hex.EncodeToString(sum[:6])
	return frozen
}

func (t *SkillTool) orderSkillList(
	ctx context.Context,
	query string,
) ([]SkillMeta, error) {
	frozen := freezeSkills(t.Loader.List())
	var (
		staticIDs  []string
		staticDone bool
	)
	static := func(context.Context) ([]string, error) {
		if !staticDone {
			staticDone = true
			staticIDs = skillIDs(rankSkills(frozen.skills, query))
		}
		return slices.Clone(staticIDs), nil
	}
	execute := func(
		ctx context.Context,
		action string,
	) ([]string, error) {
		switch action {
		case SkillRoutingCatalog:
			return slices.Clone(frozen.ids), nil
		case SkillRoutingStatic:
			return static(ctx)
		default:
			return nil, fmt.Errorf(
				"skill routing strategy %q is not registered",
				action,
			)
		}
	}
	if t.Adaptive == nil || len(frozen.skills) == 0 {
		ids, err := static(ctx)
		return frozen.selectSkills(ids), err
	}
	ids, err := t.Adaptive.OrderSkillRouting(
		ctx,
		SkillRoutingInput{
			OwnerID:         ownerFromContext(ctx),
			RequestID:       RequestIDFromContext(ctx),
			PointOrdinal:    adaptivePointOrdinal(ctx),
			QueryLength:     len(query),
			CandidateIDs:    slices.Clone(frozen.ids),
			CatalogRevision: frozen.catalogRevision,
		},
		execute,
	)
	if err != nil || !frozen.validOrderedIDs(ids) {
		ids, err = static(ctx)
	}
	return frozen.selectSkills(ids), err
}

func (frozen frozenSkillCatalog) validOrderedIDs(ids []string) bool {
	if len(ids) > len(frozen.skills) {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, registered := frozen.byName[id]; !registered {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func (frozen frozenSkillCatalog) selectSkills(
	ids []string,
) []SkillMeta {
	skills := make([]SkillMeta, 0, len(ids))
	for _, id := range ids {
		if skill, ok := frozen.byName[id]; ok {
			skills = append(skills, skill)
		}
	}
	return skills
}

func skillIDs(skills []SkillMeta) []string {
	ids := make([]string, len(skills))
	for index, skill := range skills {
		ids[index] = skill.Name
	}
	return ids
}
