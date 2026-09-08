// identity_deprovision.go implements `aura identity {deactivate|purge}` — the missing
// entry point onto the D-27 de-provisioning saga (internal/agui/deprovision.go), which has
// shipped journaled, idempotent and resumable since Phase 36 with only the cron
// grace-window sweep able to reach it. No second teardown path and no SQL DELETE: a raw
// delete cascades the Postgres catalog and leaves the identity's ArcadeDB database, its
// Garage bucket+key, its filesystem roots and its Authula user orphaned with no owner row
// left to find them by.
//
// deactivate is the reversible half (stamp deactivated_at + purge_after, kill the Authula
// sessions) and hands the identity to the seeded grace-window sweep. purge is the
// forced-purge admin entry (Deprovisioner.PurgeOne) that bypasses the grace window — the
// same saga the sweep runs, so an interrupted purge converges on a re-run instead of
// half-removing an identity.
//
// Like `aura identity create`, both verbs boot the FULL composition root (Postgres,
// ArcadeDB, Garage, the filesystem, the sandbox box) because teardown spans every plane
// provisioning built; the DB-only verbs stay on runIdentity's lighter config.LoadDB() path.
// They additionally assemble the two Authula ports buildDeprovisioner leaves nil, because
// the Authula provider is built after that seam — without them the Authula user survives
// its identity as an orphan no later run can attribute to anyone.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

const identityDeprovisionUsage = "usage: aura identity {deactivate|purge} <name|uuid> --confirm\n" +
	"  deactivate = soft-delete: stamp deactivated_at + a grace-window purge_after and kill\n" +
	"               the identity's Authula sessions; the seeded purge sweep finishes the job\n" +
	"  purge      = run the full reverse saga NOW (sandbox box, conversations, ArcadeDB\n" +
	"               database, Garage bucket+key, filesystem roots, identity row, Authula\n" +
	"               user). Irreversible, journaled, resumable — a re-run skips done steps.\n" +
	"  --confirm is mandatory. Service identities, the seeded local operator, and any\n" +
	"  identity holding the system-managed '*' capability are refused.\n" +
	"  purge <uuid> --confirm --resume-resources finishes a purge that already removed the\n" +
	"  identity row, over the resource planes an id alone still identifies. Refused while\n" +
	"  the row exists — that case belongs to the guarded purge above."

// identityKindUser is the aura.identities.kind of a provisioned human identity — the only
// kind either verb accepts. `aura-cli` and friends are kind=service: infrastructure
// principals with no provisioned planes to tear down and no business being a target.
const identityKindUser = "user"

type deprovisionVerb string

const (
	deprovisionVerbDeactivate deprovisionVerb = "deactivate"
	deprovisionVerbPurge      deprovisionVerb = "purge"
)

// errProtectedIdentity is the refusal for a target that must never be torn down. Exported
// as a sentinel so the caller classifies without matching on message text.
var errProtectedIdentity = errors.New("identity is protected and cannot be deprovisioned")

// identityLookup is the narrow read seam the resolver needs, satisfied by *identity.Store.
type identityLookup interface {
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
	GetIdentityByID(ctx context.Context, identityID string) (identity.Identity, error)
	ListCapabilities(ctx context.Context, identityID string) ([]string, error)
}

// identityDeprovisioner is the narrow seam onto the shipped saga, satisfied by
// *agui.Deprovisioner. It exists so the verb dispatch is unit-testable without Postgres,
// ArcadeDB, Garage, Docker or Authula; production always passes the real Deprovisioner.
type identityDeprovisioner interface {
	Deactivate(ctx context.Context, identityID string) error
	PurgeOne(ctx context.Context, identityID string) error
	Purge(ctx context.Context, target agui.DeprovisionTarget) error
}

var _ identityDeprovisioner = (*agui.Deprovisioner)(nil)

// parseIdentityDeprovisionArgs extracts the single target and enforces the mandatory
// --confirm, mirroring the destructive-op guard `aura chat delete` and `aura paused-states
// purge` already use. Order-independent, and one target at a time: a loop over identities
// is the caller's business, so a typo cannot take a second one down with it.
func parseIdentityDeprovisionArgs(args []string) (target string, resumeResources bool, err error) {
	confirmed := false
	for _, a := range args {
		switch {
		case a == "--confirm":
			confirmed = true
		case a == "--resume-resources":
			resumeResources = true
		case strings.HasPrefix(a, "-"):
			return "", false, fmt.Errorf("unknown flag %q", a)
		case target != "":
			return "", false, fmt.Errorf("unexpected second target %q — deprovision one identity at a time", a)
		default:
			target = a
		}
	}
	if target == "" {
		return "", false, errors.New("missing target — pass the identity's name or UUID")
	}
	if !confirmed {
		return "", false, errors.New("refusing — pass --confirm to deprovision the identity")
	}
	return target, resumeResources, nil
}

// guardResumableOrphan gates the resource-plane resumption path. MEASURED 2026-09-08: a
// purge that fails after the identity_row step leaves an ArcadeDB database, a Garage
// bucket+key or a filesystem root behind that NO other CLI path can reach, because every
// one of them resolves its target through aura.identities — the row that is already gone.
// Deprovisioner.Purge has always accepted an id-only target for exactly this case.
//
// The guard is what keeps that entry from becoming a way past guardProtectedIdentity: a
// reference must be a UUID (a name cannot identify a row that no longer exists), the
// identity row must genuinely be absent, and the seeded operator is refused outright. A
// live identity is sent back to the guarded verb rather than torn down here.
func guardResumableOrphan(ctx context.Context, lookup identityLookup, ref string) error {
	if _, err := uuid.Parse(ref); err != nil {
		return fmt.Errorf("--resume-resources needs the identity's UUID, not %q: its row is gone, so there is no name left to resolve", ref)
	}
	if ref == localSeededIdentityID {
		return fmt.Errorf("%w: %q is the seeded local operator", errProtectedIdentity, ref)
	}
	switch _, err := lookup.GetIdentityByID(ctx, ref); {
	case err == nil:
		return fmt.Errorf("identity %q still exists — drop --resume-resources and purge it through the guarded path", ref)
	case errors.Is(err, identity.ErrIdentityNotFound):
		return nil
	default:
		return err
	}
}

// runIdentityResourcePurge re-runs the reverse saga over the planes an id alone identifies.
// The target carries the id and NOTHING else on purpose: an empty IdentityName skips the
// identity_row step and an empty AuthulaUserID skips the Authula one, which is correct,
// because those two are precisely the steps that already succeeded.
func runIdentityResourcePurge(ctx context.Context, dep identityDeprovisioner, identityID string) error {
	return dep.Purge(ctx, agui.DeprovisionTarget{IdentityID: identityID})
}

// resolveDeprovisionTarget accepts either shape an operator has in hand: the UUID printed
// by `aura identity create`, or the name `aura identity list` shows. A parseable UUID goes
// down the by-id path and never the by-name one, so an identity whose NAME happens to be a
// UUID cannot be reached by accident.
func resolveDeprovisionTarget(ctx context.Context, lookup identityLookup, ref string) (identity.Identity, error) {
	if _, err := uuid.Parse(ref); err == nil {
		return lookup.GetIdentityByID(ctx, ref)
	}
	return lookup.GetIdentityByName(ctx, ref)
}

// guardProtectedIdentity refuses the three shapes that must survive any teardown. Each is
// read from the deployment rather than assumed: `aura-cli` is kind=service, the operator
// holds the '*' wildcard migration 0004 seeds and identity.Store refuses to grant or
// revoke, and localSeededIdentityID is the seeded operator the object-store resolver maps
// to the SHARED bucket — purging it would deprovision storage other identities read.
func guardProtectedIdentity(id identity.Identity, caps []string) error {
	if id.Kind != identityKindUser {
		return fmt.Errorf("%w: %q is kind %q, not a provisioned user identity", errProtectedIdentity, id.Name, id.Kind)
	}
	if id.ID == localSeededIdentityID {
		return fmt.Errorf("%w: %q is the seeded local operator", errProtectedIdentity, id.Name)
	}
	if slices.Contains(caps, identity.Wildcard) {
		return fmt.Errorf("%w: %q holds the system-managed %q capability", errProtectedIdentity, id.Name, identity.Wildcard)
	}
	return nil
}

// runIdentityDeprovision drives exactly one saga entry point and nothing else — no
// pre-delete, no post-cleanup, no second teardown path.
func runIdentityDeprovision(ctx context.Context, dep identityDeprovisioner, verb deprovisionVerb, identityID string) error {
	switch verb {
	case deprovisionVerbDeactivate:
		return dep.Deactivate(ctx, identityID)
	case deprovisionVerbPurge:
		return dep.PurgeOne(ctx, identityID)
	default:
		return fmt.Errorf("unknown deprovision verb %q", verb)
	}
}

// authulaTeardownAdapter satisfies the two agui ports buildDeprovisioner cannot wire,
// because the Authula provider is assembled after that seam. Both are thin passthroughs
// onto Authula's own CoreServices — REUSED, never reimplemented — and both are idempotent
// the way the saga requires: DeleteAllByUserID over a user with no sessions and a delete
// of an absent user each affect zero rows without erroring (Authula v1.43.0
// BunUserRepository.Delete is an unqualified DELETE ... WHERE id = ?). The Authula user's
// accounts, sessions and verifications go with it by FK ON DELETE CASCADE
// (Authula migrations/core.go).
type authulaTeardownAdapter struct{ core *authulaservices.CoreServices }

var (
	_ agui.SessionTerminator  = authulaTeardownAdapter{}
	_ agui.AuthulaUserDeleter = authulaTeardownAdapter{}
)

func (a authulaTeardownAdapter) KillSessions(ctx context.Context, authulaUserID string) error {
	return a.core.SessionService.DeleteAllByUserID(ctx, authulaUserID)
}

func (a authulaTeardownAdapter) DeleteUser(ctx context.Context, authulaUserID string) error {
	return a.core.UserService.Delete(ctx, authulaUserID)
}

// withAuthulaTeardown adds the two Authula reverse legs to the composition-root deps. A
// nil core leaves them nil and each leg nil-skips, which is the pre-cutover behaviour — but
// for a purge that means the Authula user OUTLIVES its identity, so identityDeprovision
// refuses rather than reaching here with nothing wired.
//
// DeprovisionDeps.Jobs stays nil on purpose: the in-flight jobs a JobTerminator would kill
// belong to the `aura serve` process, and this is a separate short-lived one with no handle
// on them. Deactivate's session kill is what actually blocks the identity, and the daemon's
// own jobs die with their runs.
func withAuthulaTeardown(deps agui.DeprovisionDeps, core *authulaservices.CoreServices) agui.DeprovisionDeps {
	if core == nil {
		return deps
	}
	adapter := authulaTeardownAdapter{core: core}
	deps.Sessions = adapter
	deps.AuthulaDelete = adapter
	return deps
}

// identityDeprovision implements both verbs: parse, boot the full composition root,
// resolve + guard the target, assemble the saga with its Authula legs, run it. Every
// failure prints one line to stderr and exits non-zero; success prints one `ok:` line.
func identityDeprovision(ctx context.Context, verb deprovisionVerb, args []string) {
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, "identity "+string(verb)+":", err)
		os.Exit(1)
	}
	ref, resumeResources, err := parseIdentityDeprovisionArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity "+string(verb)+":", err)
		fmt.Fprintln(os.Stderr, identityDeprovisionUsage)
		os.Exit(1)
	}
	if resumeResources && verb != deprovisionVerbPurge {
		fail(errors.New("--resume-resources applies to purge only"))
	}

	// The full boot path, for the same reason `aura identity create` takes it: teardown
	// spans Postgres, ArcadeDB, Garage, the filesystem and the sandbox box.
	chat, err := bootChatEnv(ctx)
	if err != nil {
		fail(err)
	}
	defer chat.close()

	var target identity.Identity
	if resumeResources {
		if err := guardResumableOrphan(ctx, chat.identity, ref); err != nil {
			fail(err)
		}
		target = identity.Identity{ID: ref, Name: "(row already removed)"}
	} else {
		if target, err = resolveDeprovisionTarget(ctx, chat.identity, ref); err != nil {
			fail(err)
		}
		caps, cerr := chat.identity.ListCapabilities(ctx, target.ID)
		if cerr != nil {
			fail(cerr)
		}
		if err := guardProtectedIdentity(target, caps); err != nil {
			fail(err)
		}
	}

	authulaProvider, _, err := buildAuthulaProvider(ctx, chat, localSeededIdentityID)
	if err != nil {
		fail(err)
	}
	defer func() { _ = authulaProvider.Close() }()

	deps := deprovisionDeps(chat)
	// Purge's own preflight only runs when the target names an identity row, so an id-only
	// resumption would skip the check that keeps a nil memory purger from letting the
	// ArcadeDB database survive unnoticed. Assert it here instead of inheriting the gap.
	if resumeResources && deps.Memory == nil {
		fail(errors.New("memory purger unavailable — set ARCADEDB_ADMIN_USER and ARCADEDB_ADMIN_PASSWORD before resuming a resource purge"))
	}
	dep := agui.NewDeprovisioner(withAuthulaTeardown(deps, authulaProvider.CoreServices()))

	if resumeResources {
		if err := runIdentityResourcePurge(ctx, dep, target.ID); err != nil {
			fail(err)
		}
		fmt.Printf("ok: identity %s resource planes purged\n", target.ID)
		return
	}
	if err := runIdentityDeprovision(ctx, dep, verb, target.ID); err != nil {
		fail(err)
	}
	fmt.Printf("ok: identity %s (%s) %sd\n", target.Name, target.ID, verb)
}
