package agenteval

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Driver asks the live deployment a question and returns the conversation it
// landed in. Evidence is read back separately, from the durable record: the model
// is never asked to report on itself.
type Driver interface {
	Ask(ctx context.Context, question string) (conversationID string, err error)
}

// EvidenceReader reconstructs what a conversation actually did.
type EvidenceReader interface {
	Read(ctx context.Context, conversationID string) (Evidence, error)
}

// Run asks one case and reads back what happened. The two halves are separate
// interfaces so a second driver — the AG-UI daemon the cockpit talks to — can be
// added later without touching a single case.
func Run(ctx context.Context, d Driver, r EvidenceReader, c Case) (Evidence, []string, error) {
	conv, err := d.Ask(ctx, c.Question)
	if err != nil {
		return Evidence{}, nil, fmt.Errorf("ask %q: %w", c.Name, err)
	}
	evidence, err := r.Read(ctx, conv)
	if err != nil {
		return Evidence{}, nil, fmt.Errorf("read evidence for %q (conversation %s): %w", c.Name, conv, err)
	}
	return evidence, c.Check(evidence), nil
}

// execFunc is the process seam. Both live implementations shell out to docker, so
// without it neither could be exercised without a running deployment — and the
// argument assembly, the id extraction and the RLS scoping are exactly the parts
// that fail silently when they are wrong.
type execFunc func(ctx context.Context, stdin, name string, args ...string) ([]byte, error)

func runProcess(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	//nolint:gosec // G204: name and args are package literals ("docker", "exec", …)
	// plus two regexp-validated uuids; nothing here is operator- or model-supplied.
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	// Combined, because the conversation id the CLI driver needs is in a structured
	// log line on stderr, not in the answer on stdout.
	return cmd.CombinedOutput()
}

// threadID pulls the conversation id out of the daemon's own structured log line,
// which is the only place the CLI reveals which conversation it opened.
var threadID = regexp.MustCompile(`"thread_id":"([0-9a-f-]{36})"`)

// uuidShape guards the two ids that reach the SQL template.
var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CLIDriver drives `aura shell` inside the running container. It is the real
// binary against the real Postgres, ArcadeDB, embedder, sandbox and MCP sidecars —
// only the entry point differs from the cockpit, which the AG-UI driver will cover.
//
// Temperature is forced to zero for the run. At the production 0.7 a single turn
// is a sample, not a measurement, and a gate that flaps teaches people to ignore
// it. Determinism here is worth more than fidelity to the production sampling
// temperature, which is exercised by every other live tier.
type CLIDriver struct {
	// Container is the docker container running aura. Empty means "aura".
	Container string
	// exec defaults to runProcess; tests replace it.
	exec execFunc
}

// Ask puts the question to `aura shell` in the container and returns the
// conversation it opened, read out of the turn-start log line.
func (d CLIDriver) Ask(ctx context.Context, question string) (string, error) {
	container := d.Container
	if container == "" {
		container = "aura"
	}
	run := d.exec
	if run == nil {
		run = runProcess
	}
	out, err := run(ctx, question+"\n", "docker", "exec", "-i",
		"-e", "AURA_LLM_TEMPERATURE=0", container, "aura", "shell")
	if err != nil {
		return "", fmt.Errorf("docker exec aura shell: %w (output: %s)", err, truncate(string(out), 600))
	}
	match := threadID.FindSubmatch(out)
	if match == nil {
		return "", fmt.Errorf("no thread_id in the turn output — did the turn start? output: %s", truncate(string(out), 600))
	}
	return string(match[1]), nil
}

// evidenceQuery reconstructs the turn from aura.conversation_turns, which is the
// durable record the runner writes as it goes. It runs under the owner's identity
// because the table is fail-closed under RLS: an unscoped read is not an error,
// it is an empty result, and an empty result here would read as a clean pass.
const evidenceQuery = `SET app.current_identity = '%s';
SELECT coalesce(json_agg(json_build_object(
  'seq', seq, 'role', role, 'content', left(content, 4000),
  'tools', coalesce((SELECT json_agg(tc->'function'->>'name')
                     FROM jsonb_array_elements(tool_calls) tc), '[]'::json)
) ORDER BY seq), '[]'::json)
FROM aura.conversation_turns WHERE conversation_id = '%s';`

// PostgresEvidence reads the durable turn record through psql in the database
// container, which needs no published port and no DSN plumbing on the host.
type PostgresEvidence struct {
	// Container is the postgres container. Empty means "aura-postgres".
	Container string
	// IdentityID owns the conversation; required, because RLS returns nothing
	// rather than failing when it is absent.
	IdentityID string
	// exec defaults to runProcess; tests replace it.
	exec execFunc
}

type turnRow struct {
	Seq     int      `json:"seq"`
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Tools   []string `json:"tools"`
}

// Read reconstructs the turn from the durable record, scoped to the owner.
func (p PostgresEvidence) Read(ctx context.Context, conversationID string) (Evidence, error) {
	container := p.Container
	if container == "" {
		container = "aura-postgres"
	}
	// The two ids are interpolated into the SQL, so both must be UUID-shaped. The
	// conversation id already is — it comes from a regexp that matches nothing
	// else — but the identity arrives from the environment.
	if !uuidShape.MatchString(p.IdentityID) {
		return Evidence{}, fmt.Errorf("identity id %q is not a uuid: RLS answers an unscoped read with an empty result, which would pass every case", p.IdentityID)
	}
	if !uuidShape.MatchString(conversationID) {
		return Evidence{}, fmt.Errorf("conversation id %q is not a uuid", conversationID)
	}
	run := p.exec
	if run == nil {
		run = runProcess
	}
	query := fmt.Sprintf(evidenceQuery, p.IdentityID, conversationID)
	out, err := run(ctx, "", "docker", "exec", container, "sh", "-c",
		`PGPASSWORD=$POSTGRES_PASSWORD psql -U aura -d aura -tAc `+shellQuote(query))
	if err != nil {
		return Evidence{}, fmt.Errorf("psql: %w (output: %s)", err, truncate(string(out), 300))
	}
	return parseEvidence(string(out))
}

// parseEvidence turns the psql payload into the Evidence the assertions read. The
// SET statement echoes "SET" ahead of the JSON, so the payload starts at the first
// bracket and runs to the end — which also survives an array that wraps across
// lines, where a last-non-empty-line rule would silently read "]".
func parseEvidence(raw string) (Evidence, error) {
	start := strings.Index(raw, "[")
	if start < 0 {
		return Evidence{}, fmt.Errorf("no turn array in the psql output: %s", truncate(strings.TrimSpace(raw), 300))
	}
	payload := strings.TrimSpace(raw[start:])
	var rows []turnRow
	if err := json.Unmarshal([]byte(payload), &rows); err != nil {
		return Evidence{}, fmt.Errorf("decode turn rows: %w (payload: %s)", err, truncate(payload, 300))
	}
	if len(rows) == 0 {
		return Evidence{}, fmt.Errorf("no turns recorded — the conversation is empty or scoped to another identity")
	}
	var e Evidence
	for _, row := range rows {
		switch row.Role {
		case "assistant":
			e.LLMCalls++
			e.Tools = append(e.Tools, row.Tools...)
			if text := strings.TrimSpace(row.Content); text != "" {
				e.Answer = text // the last non-empty assistant turn is the reply
			}
		case "tool":
			if n, ok := memoryFactCount(row.Content); ok {
				e.MemoryFactCounts = append(e.MemoryFactCounts, n)
			}
		}
	}
	return e, nil
}

// memoryFactCount reports how many facts a memory-search result carried. It keys
// on the payload shape rather than on which tool produced it, so a renamed or
// re-namespaced memory tool keeps being measured instead of silently dropping out
// of the gate.
func memoryFactCount(content string) (int, bool) {
	var payload struct {
		Facts []json.RawMessage `json:"facts"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &payload); err != nil {
		return 0, false
	}
	if payload.Facts == nil {
		return 0, false
	}
	return len(payload.Facts), true
}

// shellQuote wraps a string for `sh -c`, which is the only quoting this package
// does: the SQL itself is built from a literal template plus two UUIDs.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
