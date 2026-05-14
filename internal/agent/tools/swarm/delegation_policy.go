package swarmtools

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent/tools/sets"
)

const (
	DelegationFinalizationAggregate = "aggregate"
	DelegationFinalizationNoToolLLM = "no_tool_llm"
)

type DelegationPolicy struct {
	MaxWorkers          int
	Timeout             time.Duration
	FinalizationTimeout time.Duration
	ChildMaxIterations  int
	MaxResultChars      int
	Finalization        string
}

func DefaultDelegationPolicy() DelegationPolicy {
	return DelegationPolicy{
		MaxWorkers:          1,
		Timeout:             25 * time.Second,
		FinalizationTimeout: 4 * time.Second,
		ChildMaxIterations:  3,
		MaxResultChars:      12000,
		Finalization:        DelegationFinalizationAggregate,
	}
}

func LoadDelegationPolicyFromEnv() DelegationPolicy {
	policy := DefaultDelegationPolicy()
	if n, ok := envInt("SWARM_RESEARCH_MAX_WORKERS"); ok {
		policy.MaxWorkers = n
	}
	if n, ok := envInt("SWARM_RESEARCH_TIMEOUT_MS"); ok {
		policy.Timeout = time.Duration(n) * time.Millisecond
	}
	if n, ok := envInt("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS"); ok {
		policy.FinalizationTimeout = time.Duration(n) * time.Millisecond
	}
	if n, ok := envInt("SWARM_RESEARCH_CHILD_MAX_ITERATIONS"); ok {
		policy.ChildMaxIterations = n
	}
	if n, ok := envInt("SWARM_RESEARCH_MAX_RESULT_CHARS"); ok {
		policy.MaxResultChars = n
	}
	if v := strings.TrimSpace(os.Getenv("SWARM_RESEARCH_FINALIZATION")); v != "" {
		policy.Finalization = strings.ToLower(v)
	}
	return policy.Clamp()
}

func (p DelegationPolicy) Clamp() DelegationPolicy {
	if p.MaxWorkers < 1 {
		p.MaxWorkers = 1
	}
	if p.MaxWorkers > 3 {
		p.MaxWorkers = 3
	}
	if p.Timeout <= 0 || p.Timeout > 30*time.Second {
		p.Timeout = 25 * time.Second
	}
	if p.FinalizationTimeout <= 0 || p.FinalizationTimeout > 10*time.Second {
		p.FinalizationTimeout = 4 * time.Second
	}
	if p.ChildMaxIterations < 1 || p.ChildMaxIterations > 3 {
		p.ChildMaxIterations = 3
	}
	if p.MaxResultChars < 2000 || p.MaxResultChars > 12000 {
		p.MaxResultChars = 12000
	}
	if p.Finalization != DelegationFinalizationAggregate && p.Finalization != DelegationFinalizationNoToolLLM {
		p.Finalization = DelegationFinalizationAggregate
	}
	return p
}

type WorkerCatalog interface {
	HasRole(role string) bool
}

func ValidateWorkerRoles(roles []string, catalog WorkerCatalog) error {
	if catalog == nil {
		return fmt.Errorf("worker catalog unavailable")
	}
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "" {
			continue
		}
		if strings.HasSuffix(role, ".yaml") {
			return fmt.Errorf("stale worker alias %q: yaml slugs are not valid worker roles", role)
		}
		if !catalog.HasRole(role) {
			return fmt.Errorf("unknown worker role %q", role)
		}
	}
	return nil
}

type toolsetWorkerCatalog struct{}

func (toolsetWorkerCatalog) HasRole(role string) bool {
	_, ok := toolsets.RoleTools(role)
	return ok
}

func envInt(key string) (int, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return n, true
}
