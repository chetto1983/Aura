package telegram

import (
	"github.com/aura/aura/internal/api"
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
