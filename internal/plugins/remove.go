package plugins

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/skills"
)

// Remove removes an installed plugin: its connectors in one registry update, the skills whose
// content is still what the plugin wrote, its data when asked, and then its tree and record.
// A failure stops it with the record rewritten to what remains, so a second Remove finishes the
// job. It runs detached from ctx, as an install does.
func (b *Backend) Remove(ctx context.Context, actor, name string, deleteData bool) (RemoveReport, error) {
	if err := packs.ValidPluginName(name); err != nil {
		return RemoveReport{}, invalid(err)
	}
	rec, ok, err := b.d.Store.Get(name)
	if err != nil {
		return RemoveReport{}, err
	}
	if !ok {
		return RemoveReport{}, fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	ref, err := packs.ParseRef(rec.Ref)
	if err != nil {
		return RemoveReport{}, fmt.Errorf("plugin %s records ref %q: %w", name, rec.Ref, err)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.d.WriteTimeout)
	defer cancel()
	b.mu.Lock()
	defer b.mu.Unlock()

	rec.Phase = PhaseRemoving
	if err := b.d.Store.WriteRecord(rec); err != nil {
		return RemoveReport{}, err
	}
	report := RemoveReport{Name: name}
	if report.Connectors, err = b.removeConnectors(ctx, actor, rec, ref); err != nil {
		return report, err
	}
	rec.Connectors = nil
	var remaining []SkillEntry
	report.Skills, remaining = b.archiveSkills(ctx, actor, rec.Skills)
	if len(remaining) > 0 {
		rec.Skills = remaining
		return report, errors.Join(fmt.Errorf("plugin %s: %d skill(s) could not be archived", name, len(remaining)),
			b.d.Store.WriteRecord(rec))
	}
	rec.Skills = nil
	if deleteData {
		if err := b.d.PurgeData(ctx, DataDir(name, ref)); err != nil {
			return report, errors.Join(fmt.Errorf("plugin %s: delete its data: %w", name, err), b.d.Store.WriteRecord(rec))
		}
		report.DataDeleted = true
	}
	sortSteps(report.Skills)
	return report, b.d.Store.Delete(name)
}

// removeConnectors deletes, in one update, every server the plugin's source marks, the
// recorded ones included. A recorded name that another source now holds is not the plugin's
// to remove.
func (b *Backend) removeConnectors(ctx context.Context, actor string, rec Record, ref packs.Ref) ([]Step, error) {
	source := packs.SourceMarker(ref)
	var removed []string
	var steps []Step
	err := b.d.Registry.Update(ctx, actor, ActionRemove, rec.Name, "plugin remove "+rec.Ref+"@"+rec.Commit, func(doc *mcp.ManagedConfig) error {
		removed, steps = nil, nil
		for name, s := range doc.MCPServers {
			if s.Source == source {
				removed = append(removed, name)
			}
		}
		for _, name := range rec.Connectors {
			if s, ok := doc.MCPServers[name]; ok && s.Source != source {
				steps = append(steps, Step{Name: name, Outcome: OutcomeSkipped, Reason: fmt.Sprintf("now from %q, not from this plugin", s.Source)})
			}
		}
		for _, name := range removed {
			doc.RemoveServer(name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("remove the connectors of %s: %w", rec.Name, err)
	}
	slices.Sort(removed)
	for _, name := range removed {
		b.d.Unmount(name)
		steps = append(steps, Step{Name: name, Outcome: OutcomeOK})
	}
	sortSteps(steps)
	return steps, nil
}

// archiveSkills archives each recorded skill whose active content still has its install hash.
// An edited or already archived skill is the operator's now and is left alone. It returns the
// skills whose archive failed.
func (b *Backend) archiveSkills(ctx context.Context, actor string, recorded []SkillEntry) ([]Step, []SkillEntry) {
	var steps []Step
	var remaining []SkillEntry
	for _, s := range recorded {
		hash, active := b.d.Skills.ActiveHash(s.Name)
		switch {
		case !active:
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: "no longer active"})
		case hash != s.Hash:
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: "changed since install"})
		default:
			if err := b.d.Skills.Archive(ctx, s.Name, skills.AuditActor{ActorID: actor}); err != nil {
				steps = append(steps, Step{Name: s.Name, Outcome: OutcomeFailed, Reason: err.Error()})
				remaining = append(remaining, s)
				continue
			}
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeOK})
		}
	}
	return steps, remaining
}
