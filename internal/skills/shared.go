package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/chetto1983/aura/internal/skillacl"
)

// shared.go answers the ONE question acceptance criterion 5 of amendment #214 asks: which
// skills has somebody else shared with this reader, and where does each body live.
//
// It is the join of the two halves that existed separately until now — skillacl.Store says
// WHICH resource ids a reader may see, skills.CatalogStore turns those ids into rows with an
// owner and a name — plus the Layout that turns (owner, name) into the one directory the
// body may be read from. Both consumers of a shared skill go through here, so the listing
// the model reads and the tree the box holds cannot disagree about what a grant means.
//
// THE BODY IS READ FROM THE OWNER'S EXPORT, never from the owner's root. The export holds
// exactly the owner's ACTIVE skills (Materialize/Dematerialize keep it in lockstep), so an
// archived skill stops being readable by its grantees at the same moment it stops being
// readable by its owner — without a second rule saying so.
//
// AND IT IS ONE DIRECTORY PER SKILL, never the owner's export tree. That distinction is the
// whole of amendment #215: `<owner-export>` holds every skill that owner has, so handing it
// to a reader as a source — a Loader root, a materialize source, anything that walks — would
// carry the skills that were never shared along with the one that was. The unit of a grant is
// a skill, so the unit of the source is the skill's own directory.

// SharedSkill is one skill another identity has shared with the reader: its name, whose it
// is, and the single directory its body lives in.
type SharedSkill struct {
	Name    string
	OwnerID string
	Dir     string
}

// grantReader is the ACL half this join needs — declared here as the consumer's narrow view
// rather than taking *skillacl.Store, so the fail-closed test can supply a store that errors
// without a database.
type grantReader interface {
	AccessibleResourceIDs(ctx context.Context, identityID string, rt skillacl.ResourceType, perm skillacl.Perm) ([]string, error)
}

// catalogReader is the catalog half: ids to rows, under the READER's RLS, so a row that was
// not really shared resolves to nothing rather than to somebody else's skill.
type catalogReader interface {
	ListByIDs(ctx context.Context, readerID string, ids []string) ([]CatalogRow, error)
}

// SharedReader resolves the skills shared WITH one identity. A nil *SharedReader answers "no
// shared skills" from every method, which is what a deployment with no pool must answer.
type SharedReader struct {
	grants  grantReader
	catalog catalogReader
	layout  Layout
}

// NewSharedReader builds the join over the two stores and the layout. It returns nil when
// either store is missing, so the callers keep the single nil check they already need for a
// pool-free composition instead of two.
func NewSharedReader(grants grantReader, catalog catalogReader, layout Layout) *SharedReader {
	if grants == nil || catalog == nil {
		return nil
	}
	return &SharedReader{grants: grants, catalog: catalog, layout: layout}
}

// For returns the skills shared with reader, name-ordered, each with the one directory its
// body may be read from.
//
// Four filters stand between a grant row and a directory, and each drops rather than
// admits:
//
//   - a row the reader OWNS is not a share (they already read it from their own root, and
//     listing it twice would let a stale export shadow the live tree);
//   - a name the reader's own library or the deployment's already uses is dropped
//     ENTIRELY. Both consumers merge by copying a tree into a place where the reader's own
//     files already are, so a colliding share does not lose cleanly — it lands file by file
//     INSIDE the skill it collides with, and an extra file beside somebody else's SKILL.md
//     is executable instruction they never wrote. D-214-3 already says the house wins a
//     collision; this says a share never even competes;
//   - a body that is not on disk is not a skill: an archived or deleted skill keeps its
//     catalog row (archiving does not change who owns what) and must stop being readable;
//   - a name TWO owners share with this reader is dropped from both of them
//     (withoutContestedNames), for the same reason the second rule exists.
//
// A grant lookup that FAILS is an error, not an empty list. The caller decides whether to
// degrade — and the production callers do — but that decision is theirs to take visibly,
// because "the database was unreachable" and "nothing is shared with you" are the same
// silence and only one of them is true.
func (r *SharedReader) For(ctx context.Context, readerID string) ([]SharedSkill, error) {
	if r == nil || readerID == "" {
		return nil, nil
	}
	ids, err := r.grants.AccessibleResourceIDs(ctx, readerID, skillacl.ResourceSkill, skillacl.PermView)
	if err != nil {
		return nil, fmt.Errorf("skills: grants shared with %q: %w", readerID, err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.catalog.ListByIDs(ctx, readerID, ids)
	if err != nil {
		return nil, fmt.Errorf("skills: catalog rows shared with %q: %w", readerID, err)
	}
	readerRoots, err := r.layout.For(readerID)
	if err != nil {
		return nil, fmt.Errorf("skills: reader %q cannot name a root: %w", readerID, err)
	}
	out := make([]SharedSkill, 0, len(rows))
	for _, row := range rows {
		shared, ok := r.shareOf(row, readerID, readerRoots)
		if ok {
			out = append(out, shared)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return withoutContestedNames(out, readerID), nil
}

// withoutContestedNames drops every share whose name a SECOND owner also shares with the
// same reader. The catalog is unique on (owner, name) and not on name, so two identities may
// each own a `deploy` and each share it — one `--with public` grant on a common name is
// enough, and neither of them has to know the other exists.
//
// Neither consumer can hold two skills under one name, and they would disagree about which:
// the loader merges by NAME and would keep whichever dir came last, while the box merges by
// FILE into one /skills/<name>, so the two trees land on top of each other and the reader
// ends up running one person's SKILL.md beside another person's scripts. That is the same
// harm nameTaken already refuses when the collision is with the reader's own library, and it
// gets the same answer: a share never competes for a name. Choosing a winner by owner id
// would be a coin toss nobody could see, and the loser's files would still be in the box.
func withoutContestedNames(shared []SharedSkill, readerID string) []SharedSkill {
	claims := make(map[string]int, len(shared))
	for _, s := range shared {
		claims[s.Name]++
	}
	out := make([]SharedSkill, 0, len(shared))
	for _, s := range shared {
		if claims[s.Name] > 1 {
			slog.Warn("skills: two identities share a skill of this name with the same reader — neither is readable",
				"skill", s.Name, "owner_id", s.OwnerID, "reader_id", readerID)
			continue
		}
		out = append(out, s)
	}
	return out
}

// shareOf applies the per-row drop rules to one catalog row and resolves its body directory.
// The fourth rule needs the whole answer and lives in withoutContestedNames.
func (r *SharedReader) shareOf(row CatalogRow, readerID string, readerRoots Roots) (SharedSkill, bool) {
	if row.OwnerID == readerID || row.OwnerID == "" {
		return SharedSkill{}, false
	}
	// The name is joined into a filesystem path below. The catalog CHECK constraint already
	// admits only the skill grammar, but a name that reaches a path join must be validated
	// where the join happens, not where it was stored.
	if err := SanitizeName(row.Name, row.Name); err != nil {
		return SharedSkill{}, false
	}
	if nameTaken(readerRoots, row.Name) {
		return SharedSkill{}, false
	}
	ownerRoots, err := r.layout.For(row.OwnerID)
	if err != nil || ownerRoots.Export == "" {
		return SharedSkill{}, false
	}
	dir := filepath.Join(ownerRoots.Export, row.Name)
	if !isSkillDir(dir) {
		return SharedSkill{}, false
	}
	return SharedSkill{Name: row.Name, OwnerID: row.OwnerID, Dir: dir}, true
}

// nameTaken reports whether the reader's own library or the deployment's already holds a
// skill by that name. It asks the LOADER roots rather than the exports because those are the
// names the reader's model already sees; a share that loses the listing but wins the box
// would be the worst of both.
func nameTaken(readerRoots Roots, name string) bool {
	for _, root := range readerRoots.LoaderRoots() {
		if isSkillDir(filepath.Join(root, name)) {
			return true
		}
	}
	return false
}

// isSkillDir reports whether dir holds a readable SKILL.md, with Lstat so a symlinked
// SKILL.md is not a skill here for the same reason it is not one in the Loader.
func isSkillDir(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, "SKILL.md"))
	return err == nil && info.Mode().IsRegular()
}

// SharedDirs projects the resolved shares onto the directory list Loader.Config.SharedDirs
// takes.
func SharedDirs(shared []SharedSkill) []string {
	out := make([]string, 0, len(shared))
	for _, s := range shared {
		out = append(out, s.Dir)
	}
	return out
}
