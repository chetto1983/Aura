package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/retention"
	"github.com/jackc/pgx/v5/pgxpool"
)

const retentionUsage = "usage: aura retention {plan|apply --token <token>}"

type retentionRunner interface {
	Plan(context.Context) (retention.Plan, error)
	Apply(context.Context, string) (retention.ApplyReport, error)
}

func runRetention(args []string) {
	cfg := config.LoadDB()
	ctx := cliInvocationContext
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura retention:", err)
		os.Exit(exitInfra)
	}
	defer pool.Close()
	if err := runRetentionCommand(ctx, newRuntimeRetentionEngine(cfg, pool), args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "aura retention:", err)
		os.Exit(exitUsage)
	}
}

func runRetentionCommand(ctx context.Context, runner retentionRunner, args []string, output io.Writer) error {
	command := "plan"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	switch command {
	case "plan":
		if len(args) != 0 {
			return errors.New(retentionUsage)
		}
		plan, err := runner.Plan(ctx)
		if err != nil {
			return err
		}
		return encoder.Encode(plan)
	case "apply":
		flags := flag.NewFlagSet("retention apply", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		token := flags.String("token", "", "exact token returned by retention plan")
		if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *token == "" {
			return errors.New("retention apply requires --token")
		}
		report, err := runner.Apply(ctx, *token)
		if err != nil {
			return err
		}
		return encoder.Encode(report)
	default:
		return errors.New(retentionUsage)
	}
}

func newRuntimeRetentionEngine(cfg *config.Config, pool *pgxpool.Pool) *retention.Engine {
	policy := retention.DefaultPolicy(retention.EnvironmentProduction)
	if cfg.Profile == config.ProfileDev || cfg.Profile == config.ProfileLocalTrusted {
		policy.Environment = retention.EnvironmentTrustedDevelopment
	}
	policy.Version = cfg.Retention.PolicyVersion
	policy.TTL[retention.ClassTemporary] = cfg.Retention.TemporaryTTL
	policy.TTL[retention.ClassCrashArtifact] = cfg.Retention.TemporaryTTL
	policy.TTL[retention.ClassFullTrace] = cfg.Retention.ActiveFullTraceTTL(cfg.Profile)
	policy.TTL[retention.ClassMetadataTrace] = cfg.Retention.MetadataTraceTTL
	policy.TTL[retention.ClassConversation] = cfg.Retention.ConversationTTL
	policy.Disk = retention.DiskThresholds{
		Warn: cfg.Retention.DiskWarnPercent, Urgent: cfg.Retention.DiskUrgentPercent,
		StopOptional: cfg.Retention.DiskStopOptionalPercent,
	}
	policy.BatchSize = cfg.Retention.BatchSize
	policy.LeaseDuration = cfg.Retention.LeaseDuration
	policy.MaxRunDuration = cfg.Retention.MaxDuration
	local := retention.LocalArtifacts{Root: cfg.RunDir, IdentityID: identityctx.LocalOperatorIdentity}
	return &retention.Engine{
		Policy: policy, Source: local, Store: retention.NewStore(pool),
		Revalidator: local, Remover: local, Finalizer: local,
		WorkerID: "aura-retention", ActivityFreshness: 90 * time.Second,
	}
}
