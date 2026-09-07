// identity_create.go implements `aura identity create` (D-07/E2E-02): a thin CLI front
// over the IDENTICAL provisioning service the cockpit wizard uses —
// onboardingService.StartSession then Provision, the exact pair
// internal/agui/onboarding_api.go:199 calls. No new saga entry point, no relaxed
// validation, no parallel implementation (D-07). The command boots the full daemon
// composition root (bootChatEnv + the same three composites serve.go uses:
// buildArcadeMemoryProvisioner, buildAuthulaProvider, buildOnboardingService) because
// provisioning spans Postgres, Authula, ArcadeDB, Garage, the filesystem, and — since
// plan 01-01 — the per-identity sandbox box: verbs that only need the DB pool (list/get/
// grant/revoke/recover) stay on runIdentity's lighter config.LoadDB() path in identity.go.
//
// Secret discipline (T-01-03): the password and the security answer are read via the
// existing readHiddenFromStdin (recover_operator.go) — REUSED, never a second
// implementation — so neither is ever a flag, never reaches argv, and never appears in
// stdout/stderr/logs. Success prints exactly one `ok:` line plus the Telegram deep link;
// failure prints a sanitized, secret-free message to stderr and exits non-zero.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
)

const identityCreateUsage = "usage: aura identity create -email <email> -security-question <question> " +
	"[-capability <cap>]... [-operator <uuid>]\n" +
	"  provisions a second identity through the SAME saga the cockpit wizard uses\n" +
	"  (StartSession + Provision): a real ArcadeDB database, Garage bucket, filesystem\n" +
	"  roots, and per-identity sandbox box, plus a required Telegram link and mandatory\n" +
	"  first-login TOTP enrollment. Requires AURA_MUSR_ISOLATION=true under a strict\n" +
	"  AURA_PROFILE and a configured Telegram bot (TELEGRAM_BOT_TOKEN).\n" +
	"  -capability may repeat; -operator defaults to the seeded local operator.\n" +
	"  The password and the security answer are read from a hidden prompt — never a flag."

// capabilityList is a repeatable flag.Value collecting -capability in request order
// (flag.FlagSet has no built-in repeatable-string flag).
type capabilityList []string

func (c *capabilityList) String() string { return strings.Join(*c, ",") }

func (c *capabilityList) Set(v string) error {
	*c = append(*c, v)
	return nil
}

// identityCreateFlags is the parsed, typed shape of `aura identity create`'s flags — pure
// and unit-testable with no pool, no Docker, no Authula (the same seam
// parseRecoverOperatorFlags gives recover-operator).
type identityCreateFlags struct {
	email            string
	securityQuestion string
	capabilities     []string
	operator         string
}

// parseIdentityCreateFlags hand-parses the flags via a std flag.FlagSet
// (flag.ContinueOnError, output discarded so the caller owns the usage print), NOT cobra.
// An unknown flag or a trailing positional argument returns an error before any boot work
// or secret prompt runs; a missing -email/-security-question is likewise refused here so
// the operator is never asked for a password on an invocation that cannot succeed.
func parseIdentityCreateFlags(args []string) (identityCreateFlags, error) {
	var caps capabilityList
	f := identityCreateFlags{operator: localSeededIdentityID}
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.email, "email", "", "the new identity's login email (required)")
	fs.StringVar(&f.securityQuestion, "security-question", "", "the recovery security question (required)")
	fs.Var(&caps, "capability", "a capability to grant (repeatable)")
	fs.StringVar(&f.operator, "operator", f.operator, "the creating operator's identity UUID")
	if err := fs.Parse(args); err != nil {
		return identityCreateFlags{}, err
	}
	if fs.NArg() > 0 {
		return identityCreateFlags{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(f.email) == "" {
		return identityCreateFlags{}, errors.New("identity create: -email is required")
	}
	if strings.TrimSpace(f.securityQuestion) == "" {
		return identityCreateFlags{}, errors.New("identity create: -security-question is required")
	}
	f.capabilities = []string(caps)
	return f, nil
}

// onboardingCreator is the narrow seam identityCreate/runIdentityCreate depend on —
// satisfied by agui.OnboardingService. It exists so a unit test can inject a stub
// returning errIsolationDisabled/agui.ErrOnboardingDuplicate without booting a real
// environment; it is a TEST SEAM, not a second implementation (D-07) — production always
// passes the real service and calls exactly this StartSession-then-Provision pair.
type onboardingCreator interface {
	StartSession(ctx context.Context, creatorIdentityID string) (agui.OnboardingStart, error)
	Provision(ctx context.Context, requesterIdentityID, token string, in agui.OnboardingProvisionRequest) (agui.OnboardingProvisionResponse, error)
}

// runIdentityCreate drives the create sequence over any onboardingCreator: StartSession
// then Provision, in that order, and nothing else (D-07: no new saga entry point, no
// relaxed validation, no parallel implementation).
func runIdentityCreate(ctx context.Context, svc onboardingCreator, f identityCreateFlags, password, securityAnswer string) (agui.OnboardingProvisionResponse, error) {
	start, err := svc.StartSession(ctx, f.operator)
	if err != nil {
		return agui.OnboardingProvisionResponse{}, err
	}
	return svc.Provision(ctx, f.operator, start.SessionToken, agui.OnboardingProvisionRequest{
		Email:            f.email,
		Password:         password,
		SecurityQuestion: f.securityQuestion,
		SecurityAnswer:   securityAnswer,
		Capabilities:     f.capabilities,
		LinkTelegram:     true,
	})
}

// identityCreateErrorMessage maps a StartSession/Provision error onto operator-facing
// stderr text. EXPORTED sentinels are matched with errors.Is (never a string match); an
// UNEXPORTED sentinel's own message — errIsolationDisabled's fixed, secret-free operator
// guidance naming AURA_PROFILE and docs/runbooks/musr-rollout.md — is passed through
// unchanged rather than re-derived, because agui deliberately keeps it unexported and this
// package must not reconstruct or duplicate it.
func identityCreateErrorMessage(err error) string {
	if errors.Is(err, agui.ErrOnboardingDuplicate) {
		return "identity create: an identity with this email already exists"
	}
	if errors.Is(err, agui.ErrOnboardingEscalation) {
		return "identity create: " + err.Error()
	}
	return "identity create: " + err.Error()
}

// identityCreate implements `aura identity create`: parse flags, prompt for the two
// secrets (never a flag), boot the full daemon composition root, build the onboarding
// service exactly as serve.go does, and run StartSession + Provision. Every failure path
// prints a secret-free message to stderr and exits non-zero; success prints one `ok:`
// line plus the Telegram deep link and nothing else.
func identityCreate(ctx context.Context, args []string) {
	f, err := parseIdentityCreateFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, identityCreateUsage)
		os.Exit(1)
	}

	password, err := readHiddenFromStdin("identity password: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity create: read password:", err)
		os.Exit(1)
	}
	securityAnswer, err := readHiddenFromStdin("security answer: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity create: read security answer:", err)
		os.Exit(1)
	}

	// The full boot path (NOT runIdentity's config.LoadDB()-only pool): provisioning spans
	// Postgres, Authula, ArcadeDB, Garage, the filesystem, and the sandbox box.
	chat, err := bootChatEnv(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity create:", err)
		os.Exit(1)
	}
	defer chat.close()

	memory := buildArcadeMemoryProvisioner(chat.cfg)
	authulaProvider, _, err := buildAuthulaProvider(ctx, chat, localSeededIdentityID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity create:", err)
		os.Exit(1)
	}
	defer func() { _ = authulaProvider.Close() }()

	svc := buildOnboardingService(ctx, chat, authulaProvider, memory)

	resp, err := runIdentityCreate(ctx, svc, f, password, securityAnswer)
	if err != nil {
		fmt.Fprintln(os.Stderr, identityCreateErrorMessage(err))
		os.Exit(1)
	}

	fmt.Printf("ok: identity %s created\n", resp.IdentityID)
	if resp.DeepLink != "" {
		fmt.Println(resp.DeepLink)
	}
}
