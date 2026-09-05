package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/skillacl"
	"github.com/chetto1983/aura/internal/skills"
)

// skills_share.go is the operator's view of who a personal skill is shared with (amendment
// #214): `aura skills share|unshare|shares`.
//
// It is the only place in the CLI that touches aura.resource_acl, and it goes through the
// same two stores the rest of Aura does — skills.CatalogStore to turn a NAME into the id a
// grant keys on, skillacl.Store to write the grant. The name→id resolution is deliberate:
// people say "share my deploy skill", and an ACL that keyed on the name would silently point
// somewhere else the day the skill is renamed.
//
// --owner is required rather than inferred. This CLI runs as the operator, not as the person
// whose library is being shared, and guessing an owner here would hand one identity's skill
// out under another's name.

// principalPublic is the --with value that opens a skill to every identity.
const principalPublic = "public"

// skillsShare grants (grant=true) or revokes (grant=false) read access to one identity's
// skill. It reports what actually changed: a revoke that removed nothing says so, because
// "revoked" over a grant that was never there is the sentence an operator stops checking.
func skillsShare(ctx context.Context, args []string, grant bool) {
	verb := "share"
	if !grant {
		verb = "unshare"
	}
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: aura skills %s <name> --owner <uuid> --with {<uuid>|public}\n", verb)
		os.Exit(1)
	}
	name := args[0]
	owner, _ := flagValue(args[1:], "--owner")
	with, _ := flagValue(args[1:], "--with")
	if owner == "" || with == "" {
		fmt.Fprintf(os.Stderr, "skills %s: --owner and --with are required\n", verb)
		os.Exit(1)
	}

	env := bootSkills(ctx)
	defer env.close()
	catalog, acl := skillShareStores(env)

	id := resolveSharedSkillID(ctx, catalog, owner, name)
	changed, err := applyShare(ctx, acl, grant, owner, id, with)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if grant {
		fmt.Printf("ok: %q shared with %s (view)\n", name, with)
		return
	}
	if !changed {
		fmt.Printf("ok: %q was not shared with %s — nothing to revoke\n", name, with)
		return
	}
	fmt.Printf("ok: %q no longer shared with %s\n", name, with)
}

// applyShare routes the four (grant|revoke) x (identity|public) combinations. It returns
// whether a revoke removed a row; a grant is an upsert and always "changed".
func applyShare(ctx context.Context, acl *skillacl.Store, grant bool, owner, resourceID, with string) (bool, error) {
	if grant {
		if with == principalPublic {
			return true, acl.GrantPublic(ctx, owner, skillacl.ResourceSkill, resourceID, skillacl.PermView)
		}
		return true, acl.GrantToIdentity(ctx, owner, skillacl.ResourceSkill, resourceID, with, skillacl.PermView)
	}
	if with == principalPublic {
		return acl.RevokePublic(ctx, owner, skillacl.ResourceSkill, resourceID)
	}
	return acl.RevokeFromIdentity(ctx, owner, skillacl.ResourceSkill, resourceID, with)
}

// skillsShares lists the grants standing on one identity's skill — the read an operator runs
// before deciding whether to revoke.
func skillsShares(ctx context.Context, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura skills shares <name> --owner <uuid>")
		os.Exit(1)
	}
	name := args[0]
	owner, _ := flagValue(args[1:], "--owner")
	if owner == "" {
		fmt.Fprintln(os.Stderr, "skills shares: --owner is required")
		os.Exit(1)
	}

	env := bootSkills(ctx)
	defer env.close()
	catalog, acl := skillShareStores(env)

	id := resolveSharedSkillID(ctx, catalog, owner, name)
	grants, err := acl.ListGrants(ctx, owner, skillacl.ResourceSkill, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(grants) == 0 {
		fmt.Printf("ok: %q is not shared\n", name)
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PRINCIPAL\tID\tPERM")
	for _, g := range grants {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\n", g.PrincipalType, g.PrincipalID, g.Perm)
	}
	_ = w.Flush()
}

// skillShareStores builds the two stores the share legs need, exiting with the operator-
// readable reason when the pool cannot back them.
func skillShareStores(env *skillsEnv) (*skills.CatalogStore, *skillacl.Store) {
	acl, err := skillacl.NewStore(env.pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return skills.NewCatalogStore(env.pool), acl
}

// resolveSharedSkillID turns the name an operator typed into the catalog id a grant keys on,
// exiting with a readable message when the identity owns no such skill. An unknown name and
// somebody else's skill are the same answer here on purpose — telling them apart would leak
// another person's library one probe at a time.
func resolveSharedSkillID(ctx context.Context, catalog *skills.CatalogStore, owner, name string) string {
	id, err := catalog.ResolveID(ctx, owner, name)
	if err == nil {
		return id
	}
	if errors.Is(err, skills.ErrCatalogUnknownSkill) {
		fmt.Fprintf(os.Stderr, "skills: identity %s owns no skill %q (only skills written by that identity can be shared)\n", owner, name)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
	return ""
}
