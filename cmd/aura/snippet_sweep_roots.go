package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/chetto1983/aura/internal/skills"
)

// snippet_sweep_roots.go makes the snippet TTL sweep visit the roots amendment #214 created.
//
// SweepExpiredSnippets reads ONE root — the Writer's own activeDir. The Writer the sweep is
// wired with is the deployment-global one (serve_dispatch.go), so its activeDir is
// AURA_SKILLS_DIR and the per-identity roots under AURA_SKILLS_IDENTITY_DIR were never
// visited. Everything a person saves through `skill_manage action=save_snippet` lands in
// their own root, so nothing an identity ever saved could expire: the TTL applied only to
// the house library, which is the one place snippets are not saved.
//
// Measured on a live deployment 2026-09-06: after an operator saved and archived snippets
// through the tool, identities/<uuid>/ held their tree while AURA_SKILLS_DIR held only the
// four skills the deployment ships — the sole directory the sweep was reading.
//
// The loop lives at the composition root rather than inside Writer, for the reason the
// asset-processing worker and the approval expirer already loop here: this is the layer that
// knows both the layout and who exists. Writer.For is the same seam the model path uses, so a
// swept identity gets its OWN audit row under its OWN identity transaction — which the
// global sweep could not have written even if it had found the file.
//
// Roots are enumerated from the FILESYSTEM, not from aura.identities. What must be swept is a
// directory that exists; an identity with no root has nothing to sweep, and a root left by a
// deleted identity is exactly the one nobody else will ever come back for. Writer.For
// re-validates each name through idroot, so a directory that is not a legal identity is
// refused there rather than trusted here.

// sweepIdentitySnippetRoots runs the TTL sweep over every per-identity root and returns the
// combined counts. It is best-effort per root: one unreadable identity must not cost the
// others their sweep, which is the same log-and-continue posture Writer keeps per skill.
func sweepIdentitySnippetRoots(
	ctx context.Context,
	w *skills.Writer,
	identitiesDir string,
	ttl time.Duration,
	now time.Time,
	actor skills.AuditActor,
) (archived, kept []string) {
	if w == nil || identitiesDir == "" || ttl <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(identitiesDir)
	if err != nil {
		// A base that does not exist yet is a deployment with no per-identity skills, not a
		// fault: no identity has saved anything. Anything else is worth a line, because the
		// consequence is silent — snippets that never expire.
		if !os.IsNotExist(err) {
			slog.Warn("snippet TTL sweep: per-identity roots unreadable, only the house library was swept",
				"dir", identitiesDir, "err", err)
		}
		return nil, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		scoped, err := w.For(e.Name())
		if err != nil {
			slog.Warn("snippet TTL sweep: skipping a root that is not a valid identity",
				"entry", e.Name(), "err", err)
			continue
		}
		// For returns the receiver unchanged when the layout has no identity base or the
		// name resolves to no own root. Sweeping that would re-sweep the house library once
		// per directory and double-count every skill in it.
		if scoped.ActiveDir() == w.ActiveDir() {
			continue
		}
		res, err := scoped.SweepExpiredSnippets(ctx, ttl, now, actor)
		if err != nil {
			slog.Warn("snippet TTL sweep: identity root failed", "identity", e.Name(), "err", err)
			continue
		}
		archived = append(archived, res.Archived...)
		kept = append(kept, res.Kept...)
	}
	return archived, kept
}
