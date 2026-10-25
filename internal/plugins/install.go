package plugins

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/skills"
)

// What happened to one skill or connector of an install or a removal.
const (
	OutcomeOK        = "ok"
	OutcomeSkipped   = "skipped"
	OutcomeFailed    = "failed"
	OutcomeAttention = "attention"
)

// The registry audit actions of a plugin's one write each way.
const (
	ActionInstall = "plugin-install"
	ActionRemove  = "plugin-remove"
	ActionUpgrade = "plugin-upgrade"
)

const defaultWriteTimeout = 10 * time.Minute

// SkillLibrary is the deployment's skill library, as an install and a removal use it.
type SkillLibrary interface {
	// Exists reports an active or an archived skill of that name.
	Exists(name string) bool
	InstallDir(ctx context.Context, dir, source string, actor skills.AuditActor) (hash string, err error)
	ActiveHash(name string) (string, bool)
	Delete(ctx context.Context, name string, actor skills.AuditActor) error
}

// Registry applies one change to the MCP registry: fn runs inside the registry's lock on a
// freshly loaded document, which is saved with one audit row when fn returns nil. Load reads
// the document as it stands.
type Registry interface {
	Update(ctx context.Context, actor, action, subject, reason string, fn func(*mcp.ManagedConfig) error) error
	Load() (mcp.ManagedConfig, error)
}

// Deps is everything the backend reaches outside this package.
type Deps struct {
	Store    Store
	Checkout func(ctx context.Context, ref packs.Ref, commit string) (packs.Checkout, error)
	// Head reads the commit at the tip of the ref's default branch without a checkout.
	Head     func(ctx context.Context, ref packs.Ref) (string, error)
	Skills   SkillLibrary
	Registry Registry
	// Verify brings a stdio connector up in the box before it is saved, and returns the entry
	// to save.
	Verify  func(ctx context.Context, name string, s mcp.ManagedServer) (mcp.ManagedServer, error)
	Mount   func(ctx context.Context, name string, s mcp.ManagedServer)
	Unmount func(name string)
	// Probe reaches a remote connector after it is saved; one waiting for an authorization fails.
	Probe     func(ctx context.Context, name string, s mcp.ManagedServer) mcp.ProbeResult
	PurgeData func(ctx context.Context, dataDir string) error
	// Admins reports whether an identity may install and remove plugins.
	Admins func(ctx context.Context, identityID string) (bool, error)
	// Taken reads the names in use now; an unreadable registry fails the review.
	Taken        func() (Taken, error)
	Blocklist    []string
	BodyCapBytes int
	// WriteTimeout bounds an install's or a removal's writing phase, which runs detached from
	// the request. Zero means ten minutes.
	WriteTimeout time.Duration
	Now          func() time.Time
}

// Backend is the one implementation of plugin review, install and removal, behind the cockpit
// and the chat alike.
type Backend struct {
	d Deps
	// mu serializes the writing phases, from the digest check on. Reads never take it.
	mu sync.Mutex
}

// New returns a backend over d.
func New(d Deps) *Backend {
	if d.WriteTimeout == 0 {
		d.WriteTimeout = defaultWriteTimeout
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Backend{d: d}
}

// InstallRequest is what an operator approved: one plugin, at one commit, as reviewed.
type InstallRequest struct {
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	Digest string `json:"digest"`
}

// Step is the outcome of one skill or connector. A connector step needs the operator when its
// outcome is attention: the server is saved and mounted but did not answer its probe, and its
// authorization lives on the MCP board.
type Step struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// Report is the account of an install.
type Report struct {
	Review     Review `json:"review"`
	Skills     []Step `json:"skills"`
	Connectors []Step `json:"connectors"`
}

// RemoveReport is the account of a removal.
type RemoveReport struct {
	Name        string `json:"name"`
	Skills      []Step `json:"skills"`
	Connectors  []Step `json:"connectors"`
	DataDeleted bool   `json:"data_deleted"`
}

// Started reports whether the install wrote anything before an error stopped it: the tree is
// claimed, so the report says what landed and the operator needs it beside the error.
func (r Report) Started() bool { return r.Review.Name != "" }

// Started reports whether the removal wrote anything before an error stopped it: the record is
// marked removing, so the report says what left and a second removal finishes the job.
func (r RemoveReport) Started() bool { return r.Name != "" }

// Review checks the plugin out at its default branch and reviews it. It writes nothing.
func (b *Backend) Review(ctx context.Context, ref string) (Review, error) {
	r, err := packs.ParseRef(ref)
	if err != nil {
		return Review{}, invalid(err)
	}
	c, err := b.d.Checkout(ctx, r, "")
	if err != nil {
		return Review{}, err
	}
	defer func() { _ = c.Close() }()
	taken, err := b.d.Taken()
	if err != nil {
		return Review{}, err
	}
	return BuildReview(c, r, taken, b.d.Blocklist, b.d.BodyCapBytes)
}

// List returns the installed plugins.
func (b *Backend) List() ([]Record, error) { return b.d.Store.List() }

// Get returns one installed plugin's record.
func (b *Backend) Get(name string) (Record, bool, error) { return b.d.Store.Get(name) }

// MayAdminister reports whether an identity may install and remove plugins.
func (b *Backend) MayAdminister(ctx context.Context, identityID string) (bool, error) {
	return b.d.Admins(ctx, identityID)
}

// Install installs the plugin req names at the reviewed commit, when what it would write still
// has the reviewed digest. Each skill and connector lands or is reported; the record names only
// what landed. Past the digest check the install runs detached from ctx, so a closed tab does
// not leave half a plugin.
func (b *Backend) Install(ctx context.Context, actor string, req InstallRequest) (Report, error) {
	ref, err := packs.ParseRef(req.Ref)
	if err != nil {
		return Report{}, invalid(err)
	}
	if err := packs.ValidCommit(req.Commit); err != nil {
		return Report{}, invalid(err)
	}
	if req.Digest == "" {
		return Report{}, invalid(errors.New("no review digest"))
	}
	c, err := b.d.Checkout(ctx, ref, req.Commit)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = c.Close() }()

	b.mu.Lock()
	defer b.mu.Unlock()
	pack, err := onePack(c, ref)
	if err != nil {
		return Report{}, err
	}
	if _, installed, err := b.d.Store.Get(pack.Name); err != nil || installed {
		return Report{}, errors.Join(err, alreadyInstalled(installed, pack.Name))
	}
	taken, err := b.d.Taken()
	if err != nil {
		return Report{}, err
	}
	review, err := BuildReview(c, ref, taken, b.d.Blocklist, b.d.BodyCapBytes)
	if err != nil {
		return Report{}, err
	}
	if review.Digest != req.Digest {
		return Report{}, ErrDigestChanged
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.d.WriteTimeout)
	defer cancel()
	return b.install(ctx, actor, ref, review, filepath.Join(c.Root, filepath.FromSlash(pack.Directory)))
}

func invalid(err error) error { return fmt.Errorf("%w: %w", ErrInvalidRequest, err) }

func alreadyInstalled(installed bool, name string) error {
	if !installed {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrAlreadyInstalled, name)
}

func (b *Backend) install(ctx context.Context, actor string, ref packs.Ref, review Review, root string) (Report, error) {
	now := b.d.Now().UTC().Format(time.RFC3339)
	rec := Record{
		Name: review.Name, Ref: review.Ref, Commit: review.Commit, Digest: review.Digest,
		Version: review.Version, Format: review.Format, InstalledBy: actor, InstalledAt: now,
		Skills: review.Skills, Connectors: connectorNames(review.Connectors), Phase: PhaseInstalling,
	}
	if err := b.d.Store.Claim(rec, root); err != nil {
		return Report{}, err
	}
	report := Report{Review: review}
	rec.Skills, report.Skills = b.installSkills(ctx, actor, ref, review)
	if err := b.d.Store.WriteRecord(rec); err != nil {
		return report, err
	}
	trust := mcp.ManagedTrust{ApprovedBy: actor, ApprovedAt: now, Reason: installReason(review)}
	taken, err := b.d.Taken()
	if err != nil {
		rec.Connectors = nil
		return b.finishInstall(rec, report, err)
	}
	ready, steps := b.verifyConnectors(ctx, review.Connectors, taken, trust)
	saved, steps, err := b.saveConnectors(ctx, actor, ActionInstall, installReason(review), review, ready, steps, "")
	rec.Connectors = connectorNames(saved)
	report.Connectors = steps
	if err != nil {
		return b.finishInstall(rec, report, err)
	}
	report.Connectors = append(report.Connectors, b.mountConnectors(ctx, saved)...)
	return b.finishInstall(rec, report, nil)
}

func installReason(r Review) string { return "plugin install " + r.Ref + "@" + r.Commit }

// finishInstall writes the final record, with only what landed, or deletes the plugin when
// nothing did.
func (b *Backend) finishInstall(rec Record, report Report, cause error) (Report, error) {
	sortSteps(report.Skills)
	sortSteps(report.Connectors)
	if len(rec.Skills) == 0 && len(rec.Connectors) == 0 {
		return report, errors.Join(cause, b.d.Store.Delete(rec.Name))
	}
	rec.Phase = PhaseInstalled
	return report, errors.Join(cause, b.d.Store.WriteRecord(rec))
}

func (b *Backend) installSkills(ctx context.Context, actor string, ref packs.Ref, review Review) ([]SkillEntry, []Step) {
	steps := skippedSteps(review.SkippedSkills)
	var landed []SkillEntry
	for _, s := range review.Skills {
		if b.d.Skills.Exists(s.Name) {
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: "a skill with this name already exists (active or archived)"})
			continue
		}
		dir := filepath.Join(b.d.Store.TreeDir(review.Name), "skills", s.Name)
		hash, err := b.d.Skills.InstallDir(ctx, dir, packs.SourceMarker(ref), skills.AuditActor{ActorID: actor})
		if err != nil {
			steps = append(steps, Step{Name: s.Name, Outcome: OutcomeFailed, Reason: err.Error()})
			continue
		}
		landed = append(landed, SkillEntry{Name: s.Name, Hash: hash})
		steps = append(steps, Step{Name: s.Name, Outcome: OutcomeOK})
	}
	return landed, steps
}

// verifyConnectors approves each reviewed connector by this confirmation and brings each stdio
// one up in the box. A name taken since the review, or a failed start, leaves the connector out.
func (b *Backend) verifyConnectors(ctx context.Context, reviewed []Connector, taken Taken, trust mcp.ManagedTrust) ([]Connector, []Step) {
	var ready []Connector
	var steps []Step
	for _, c := range reviewed {
		if source, ok := taken.Server(c.Name); ok {
			steps = append(steps, Step{Name: c.Name, Outcome: OutcomeSkipped, Reason: takenReason(source)})
			continue
		}
		c.server.Trust = trust
		c.server.Trust.Class = c.Trust
		if c.Placement == PlacementBox {
			verified, err := b.d.Verify(ctx, c.Name, c.server)
			if err != nil {
				steps = append(steps, Step{Name: c.Name, Outcome: OutcomeFailed, Reason: err.Error()})
				continue
			}
			c.server = verified
		}
		ready = append(ready, c)
	}
	return ready, steps
}

// saveConnectors writes every ready connector in one registry update, which re-checks each name
// inside the registry lock. A non-empty replace first deletes every row from that source, so an
// upgrade swaps a plugin's connectors in the same write.
func (b *Backend) saveConnectors(ctx context.Context, actor, action, reason string, review Review, ready []Connector, steps []Step, replace string) ([]Connector, []Step, error) {
	if len(ready) == 0 && replace == "" {
		return nil, append(steps, skippedSteps(review.SkippedConnectors)...), nil
	}
	var saved []Connector
	var taken []Step
	err := b.d.Registry.Update(ctx, actor, action, review.Name, reason, func(doc *mcp.ManagedConfig) error {
		saved, taken = nil, nil
		for name, s := range doc.MCPServers {
			if replace != "" && s.Source == replace {
				doc.RemoveServer(name)
			}
		}
		for _, c := range ready {
			if existing, ok := doc.MCPServers[c.Name]; ok {
				taken = append(taken, Step{Name: c.Name, Outcome: OutcomeSkipped, Reason: takenReason(existing.Source)})
				continue
			}
			if doc.MCPServers == nil {
				doc.MCPServers = map[string]mcp.ManagedServer{}
			}
			doc.MCPServers[c.Name] = c.server
			doc.AddToProfile(doc.ActiveProfileName(), c.Name)
			saved = append(saved, c)
		}
		return nil
	})
	steps = append(steps, skippedSteps(review.SkippedConnectors)...)
	if err != nil {
		for _, c := range ready {
			steps = append(steps, Step{Name: c.Name, Outcome: OutcomeFailed, Reason: err.Error()})
		}
		return nil, steps, fmt.Errorf("save the connectors of %s: %w", review.Name, err)
	}
	return saved, append(steps, taken...), nil
}

// mountConnectors mounts each saved connector, then probes each remote one: a remote server
// that needs an authorization cannot answer until the operator grants it.
func (b *Backend) mountConnectors(ctx context.Context, saved []Connector) []Step {
	steps := make([]Step, 0, len(saved))
	for _, c := range saved {
		b.d.Mount(ctx, c.Name, c.server)
	}
	for _, c := range saved {
		if c.Placement != PlacementRemote {
			steps = append(steps, Step{Name: c.Name, Outcome: OutcomeOK})
			continue
		}
		if res := b.d.Probe(ctx, c.Name, c.server); !res.OK {
			steps = append(steps, Step{Name: c.Name, Outcome: OutcomeAttention, Reason: probeReason(res)})
			continue
		}
		steps = append(steps, Step{Name: c.Name, Outcome: OutcomeOK})
	}
	return steps
}

func probeReason(res mcp.ProbeResult) string {
	if res.Err != "" {
		return res.Err
	}
	return res.Detail
}

func connectorNames(cs []Connector) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func skippedSteps(skipped []packs.Skipped) []Step {
	steps := make([]Step, 0, len(skipped))
	for _, s := range skipped {
		steps = append(steps, Step{Name: s.Name, Outcome: OutcomeSkipped, Reason: s.Reason})
	}
	return steps
}

func sortSteps(steps []Step) {
	slices.SortStableFunc(steps, func(a, b Step) int { return strings.Compare(a.Name, b.Name) })
}
