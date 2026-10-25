package plugins

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/skills"
)

// ErrUpgradeRolledBack marks an upgrade that stopped before its registry write and put the old
// version back: nothing the plugin had changed.
var ErrUpgradeRolledBack = errors.New("upgrade rolled back")

// UpgradeReport is the account of one plugin's upgrade, as Codex's upgrade outcome is of one
// marketplace.
type UpgradeReport struct {
	Name        string `json:"name"`
	FromVersion string `json:"from_version"`
	FromCommit  string `json:"from_commit"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	UpToDate    bool   `json:"up_to_date"`
	// Applied reports that the new connectors were written to the registry: past that write an
	// upgrade does not roll back.
	Applied    bool   `json:"applied"`
	Skills     []Step `json:"skills"`
	Connectors []Step `json:"connectors"`
}

// Started reports whether the upgrade wrote the registry before an error stopped it, so the
// report says what landed beside the error.
func (r UpgradeReport) Started() bool { return r.Applied }

// Upgrade moves an installed plugin to the head of its ref's default branch, the way Codex
// upgrades a Git marketplace (codex-rs/core-plugins/src/marketplace_upgrade.rs) and then
// reinstalls its plugins (ForceReinstall):
//   - one ls-remote first, and nothing more when the head is the recorded commit;
//   - otherwise the head is checked out, the new tree swapped in with the old one kept as a
//     backup, and the plugin reinstalled from it;
//   - a new connector that fails its handshake, or a registry write that fails, puts the backup
//     and the old record back and remounts the old connectors.
//
// The operator approved the plugin's source at install; an upgrade reads the same ref, so it
// asks nothing, as Codex does not.
func (b *Backend) Upgrade(ctx context.Context, actor, name string) (UpgradeReport, error) {
	rec, ref, err := b.upgradable(name)
	if err != nil {
		return UpgradeReport{}, err
	}
	head, err := b.d.Head(ctx, ref)
	if err != nil {
		return UpgradeReport{}, err
	}
	if upToDate(rec, head) {
		return upToDateReport(rec), nil
	}
	c, err := b.d.Checkout(ctx, ref, head)
	if err != nil {
		return UpgradeReport{}, err
	}
	defer func() { _ = c.Close() }()

	b.mu.Lock()
	defer b.mu.Unlock()
	// Read again inside the lock: an upgrade that waited behind another acts on what it left.
	if rec, _, err = b.upgradable(name); err != nil {
		return UpgradeReport{}, err
	}
	if upToDate(rec, head) {
		return upToDateReport(rec), nil
	}
	pack, err := onePack(c, ref)
	if err != nil {
		return UpgradeReport{}, err
	}
	if pack.Name != name {
		return UpgradeReport{}, invalid(fmt.Errorf("%s now holds plugin %q, not %q: remove %s and install it", ref, pack.Name, name, name))
	}
	taken, err := b.d.Taken()
	if err != nil {
		return UpgradeReport{}, err
	}
	review, err := BuildReview(c, ref, ownNames(taken, rec, ref), b.d.Blocklist, b.d.BodyCapBytes)
	if err != nil {
		return UpgradeReport{}, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.d.WriteTimeout)
	defer cancel()
	return b.upgrade(ctx, actor, ref, rec, review, filepath.Join(c.Root, filepath.FromSlash(pack.Directory)))
}

// UpgradeAll upgrades every installed plugin in turn. One that fails does not stop the others,
// as Codex's outcome carries one error per marketplace.
func (b *Backend) UpgradeAll(ctx context.Context, actor string) ([]UpgradeReport, error) {
	records, err := b.d.Store.List()
	if err != nil {
		return nil, err
	}
	reports := make([]UpgradeReport, 0, len(records))
	var errs []error
	for _, rec := range records {
		report, err := b.Upgrade(ctx, actor, rec.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rec.Name, err))
		}
		if report.Name == "" {
			report.Name = rec.Name
		}
		reports = append(reports, report)
	}
	return reports, errors.Join(errs...)
}

// upgradable reads the record of a plugin an upgrade may act on.
func (b *Backend) upgradable(name string) (Record, packs.Ref, error) {
	if err := packs.ValidPluginName(name); err != nil {
		return Record{}, packs.Ref{}, invalid(err)
	}
	rec, ok, err := b.d.Store.Get(name)
	if err != nil {
		return Record{}, packs.Ref{}, err
	}
	if !ok {
		return Record{}, packs.Ref{}, fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	if rec.Phase == PhaseRemoving {
		return Record{}, packs.Ref{}, fmt.Errorf("plugin %s is being removed: remove it again to finish, then install it", name)
	}
	ref, err := packs.ParseRef(rec.Ref)
	if err != nil {
		return Record{}, packs.Ref{}, fmt.Errorf("plugin %s records ref %q: %w", name, rec.Ref, err)
	}
	return rec, ref, nil
}

// upToDate holds for a finished install at head. A record left in another phase at head is an
// upgrade or an install that stopped, which the next upgrade finishes.
func upToDate(rec Record, head string) bool {
	return rec.Commit == head && rec.Phase == PhaseInstalled
}

func upToDateReport(rec Record) UpgradeReport {
	return UpgradeReport{
		Name: rec.Name, FromVersion: rec.Version, FromCommit: rec.Commit,
		Version: rec.Version, Commit: rec.Commit, UpToDate: true,
	}
}

func (b *Backend) upgrade(ctx context.Context, actor string, ref packs.Ref, old Record, review Review, root string) (UpgradeReport, error) {
	report := UpgradeReport{
		Name: old.Name, FromVersion: old.Version, FromCommit: old.Commit,
		Version: review.Version, Commit: review.Commit,
	}
	doc, err := b.d.Registry.Load()
	if err != nil {
		return report, fmt.Errorf("read the MCP registry: %w", err)
	}
	source := packs.SourceMarker(ref)
	previous := map[string]mcp.ManagedServer{}
	for name, s := range doc.MCPServers {
		if s.Source == source {
			previous[name] = s
		}
	}
	// A connector runs from the kept tree: none may run while the tree is replaced.
	for _, name := range slices.Sorted(maps.Keys(previous)) {
		b.d.Unmount(name)
	}
	if err := b.d.Store.Swap(old.Name, root); err != nil {
		return report, b.rollback(ctx, old, previous, err)
	}
	next := old
	next.Commit, next.Phase = review.Commit, PhaseUpgrading
	if err := b.d.Store.WriteRecord(next); err != nil {
		return report, b.rollback(ctx, old, previous, err)
	}

	reason := "plugin upgrade " + review.Ref + "@" + review.Commit
	trust := mcp.ManagedTrust{ApprovedBy: actor, ApprovedAt: b.d.Now().UTC().Format(time.RFC3339), Reason: reason}
	taken, err := b.d.Taken()
	if err != nil {
		return report, b.rollback(ctx, old, previous, err)
	}
	ready, steps := b.verifyConnectors(ctx, review.Connectors, ownNames(taken, old, ref), trust)
	if i := slices.IndexFunc(steps, func(s Step) bool { return s.Outcome == OutcomeFailed }); i >= 0 {
		return report, b.rollback(ctx, old, previous, fmt.Errorf("connector %s: %s", steps[i].Name, steps[i].Reason))
	}
	saved, steps, err := b.saveConnectors(ctx, actor, ActionUpgrade, reason, review, ready, steps, source)
	if err != nil {
		return report, b.rollback(ctx, old, previous, err)
	}
	report.Applied = true
	report.Connectors = append(steps, b.mountConnectors(ctx, saved)...)
	for _, name := range slices.Sorted(maps.Keys(previous)) {
		if !slices.ContainsFunc(saved, func(c Connector) bool { return c.Name == name }) {
			report.Connectors = append(report.Connectors, Step{Name: name, Outcome: OutcomeOK, Reason: "removed"})
		}
	}

	next.Skills, report.Skills = b.upgradeSkills(ctx, actor, ref, old.Skills, review)
	next.Connectors = connectorNames(saved)
	next.Version, next.Digest, next.Format, next.Phase = review.Version, review.Digest, review.Format, PhaseInstalled
	sortSteps(report.Skills)
	sortSteps(report.Connectors)
	return report, errors.Join(b.d.Store.WriteRecord(next), b.d.Store.DropBackup(old.Name))
}

// rollback puts the backup tree and the old record back and remounts the old connectors, as
// activate_marketplace_root restores its backup when a step after the swap fails.
func (b *Backend) rollback(ctx context.Context, old Record, previous map[string]mcp.ManagedServer, cause error) error {
	undo := errors.Join(b.d.Store.Restore(old.Name), b.d.Store.WriteRecord(old))
	for _, name := range slices.Sorted(maps.Keys(previous)) {
		b.d.Mount(ctx, name, previous[name])
	}
	return errors.Join(fmt.Errorf("%w: %s stays at %s (%s): %w", ErrUpgradeRolledBack, old.Name, old.Version, old.Commit, cause), undo)
}

// upgradeSkills reinstalls the plugin's skills from the new tree, as Codex's ForceReinstall
// does, with one difference the install made already (install spec §4.7): a skill the operator
// changed since the install is theirs and stays. A skill the new version dropped is deleted
// while it is still as installed. It returns the skills the record keeps.
func (b *Backend) upgradeSkills(ctx context.Context, actor string, ref packs.Ref, recorded []SkillEntry, review Review) ([]SkillEntry, []Step) {
	was := make(map[string]SkillEntry, len(recorded))
	for _, s := range recorded {
		was[s.Name] = s
	}
	steps := skippedSteps(review.SkippedSkills)
	var kept []SkillEntry
	for _, s := range review.SkippedSkills {
		if old, owned := was[s.Name]; owned {
			kept = append(kept, old)
			delete(was, s.Name)
		}
	}
	for _, s := range review.Skills {
		old, owned := was[s.Name]
		delete(was, s.Name)
		if owned {
			hash, active := b.d.Skills.ActiveHash(s.Name)
			switch {
			case !active:
				steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: "no longer active"})
				continue
			case hash != old.Hash:
				steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: "changed since install"})
				kept = append(kept, old)
				continue
			case hash == s.Hash:
				steps = append(steps, Step{Name: s.Name, Outcome: OutcomeOK, Reason: "unchanged"})
				kept = append(kept, old)
				continue
			}
		} else if b.d.Skills.Exists(s.Name) {
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: "a skill with this name already exists (active or archived)"})
			continue
		}
		dir := filepath.Join(b.d.Store.TreeDir(review.Name), "skills", s.Name)
		hash, err := b.d.Skills.InstallDir(ctx, dir, packs.SourceMarker(ref), skills.AuditActor{ActorID: actor})
		if err != nil {
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeFailed, Reason: err.Error()})
			if owned {
				kept = append(kept, old)
			}
			continue
		}
		kept = append(kept, SkillEntry{Name: s.Name, Hash: hash})
		landed := "new"
		if owned {
			landed = "updated"
		}
		steps = append(steps, Step{Name: s.Name, Outcome: OutcomeOK, Reason: landed})
	}
	dropped := make([]SkillEntry, 0, len(was))
	for _, name := range slices.Sorted(maps.Keys(was)) {
		dropped = append(dropped, was[name])
	}
	deleted, remaining := b.deleteSkills(ctx, actor, dropped)
	for i := range deleted {
		if deleted[i].Outcome == OutcomeOK {
			deleted[i].Reason = "removed"
		}
	}
	return append(kept, remaining...), append(steps, deleted...)
}

// ownNames frees the names an installed plugin holds, so its new version's review counts them
// as its own: the skills its record lists and the registry rows its source marks.
func ownNames(t Taken, rec Record, ref packs.Ref) Taken {
	own := make(map[string]bool, len(rec.Skills))
	for _, s := range rec.Skills {
		own[s.Name] = true
	}
	source := packs.SourceMarker(ref)
	return Taken{
		Skill: func(name string) bool { return !own[name] && t.Skill(name) },
		Server: func(name string) (string, bool) {
			held, ok := t.Server(name)
			if ok && held == source {
				return "", false
			}
			return held, ok
		},
	}
}
