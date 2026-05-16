package main

import (
	"context"

	"github.com/aura/aura/internal/agent/tools/attempts"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/learning"
	auraskills "github.com/aura/aura/internal/skills"
)

// skillsDeleterAdapter bridges auraskills.FSDeleter (which surfaces a
// package-internal not-found sentinel) to api.SkillDeleter (which
// expects api.ErrSkillNotFound for 404 routing). Keeping the cycle out
// of the two packages costs us this 8-line shim.
type skillsDeleterAdapter struct {
	inner *auraskills.FSDeleter
}

func (a skillsDeleterAdapter) Delete(name string) error {
	if a.inner == nil {
		return api.ErrSkillNotFound
	}
	if err := a.inner.Delete(name); err != nil {
		if auraskills.IsSkillNotFound(err) {
			return api.ErrSkillNotFound
		}
		return err
	}
	return nil
}

type skillProposalApplierAdapter struct {
	inner *auraskills.FSProposalApplier
}

func (a skillProposalApplierAdapter) ApplySkillProposal(ctx context.Context, proposal api.SkillProposalApplyRequest) error {
	if a.inner == nil {
		return api.ErrSkillNotFound
	}
	return a.inner.ApplySkillProposal(ctx, auraskills.LocalProposal{
		Action:  proposal.Action,
		Name:    proposal.Name,
		Content: proposal.Content,
	})
}

func newSkillsDeleterAdapter(d *auraskills.FSDeleter) api.SkillDeleter {
	return skillsDeleterAdapter{inner: d}
}

func newSkillProposalApplierAdapter(p *auraskills.FSProposalApplier) api.SkillProposalApplier {
	return skillProposalApplierAdapter{inner: p}
}

// lessonPromoterAdapter bridges learning.PromoteLessons to cron.LessonPromoter.
type lessonPromoterAdapter struct {
	attemptsRepo  *attempts.SQLiteRepo
	proposalStore *learning.SQLProposalStore
}

func (a *lessonPromoterAdapter) Promote(ctx context.Context) (promoted, skipped int, err error) {
	return learning.PromoteLessons(ctx, a.attemptsRepo, a.proposalStore, 3, 7)
}
