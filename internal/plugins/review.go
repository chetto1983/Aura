package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/skills"
)

// The install's refusals. ErrDigestChanged is never the repository moving: the install reads
// the pinned commit.
var (
	// ErrInvalidRequest marks a ref, commit or name the caller can correct.
	ErrInvalidRequest   = errors.New("invalid plugin request")
	ErrAlreadyInstalled = errors.New("plugin already installed")
	ErrNotInstalled     = errors.New("plugin not installed")
	ErrDigestChanged    = errors.New("what this install would write changed since the review — a skill or connector " +
		"name was taken in the meantime, or Aura was updated; the pinned commit did not move. Review the plugin again")
)

// Where a connector runs.
const (
	PlacementBox    = "box"
	PlacementRemote = "remote"
)

// Connector is one connector the install would save, as the operator reviews it. It keeps the
// staged entry, so the install saves exactly what was reviewed.
type Connector struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Placement string `json:"placement"`
	Trust     string `json:"trust"`
	// OAuth reports that the connector may ask for an authorization: a Claude `oauth` block,
	// or any remote server, since Agent Plugins leaves authorization to the client (§7.2.1).
	OAuth  bool `json:"oauth"`
	server mcp.ManagedServer
}

// Review is everything an install of one plugin at one commit would write, and what it would
// leave out and why. It writes nothing.
type Review struct {
	Ref               string          `json:"ref"`
	Commit            string          `json:"commit"`
	Digest            string          `json:"digest"`
	Name              string          `json:"name"`
	Version           string          `json:"version"`
	Author            string          `json:"author"`
	Format            string          `json:"format"`
	Skills            []SkillEntry    `json:"skills"`
	SkippedSkills     []packs.Skipped `json:"skipped_skills"`
	Connectors        []Connector     `json:"connectors"`
	SkippedConnectors []packs.Skipped `json:"skipped_connectors"`
	Notes             []string        `json:"notes"`
	MCPDisabled       string          `json:"mcp_disabled"`
}

// Taken answers whether a name is already in use. Skill covers active and archived skills, so
// a plugin never overwrites one an operator archived; Server covers registry rows and catalog
// recipes, whose default-on servers have no registry row to collide with.
type Taken struct {
	Skill  func(name string) bool
	Server func(name string) (source string, taken bool)
}

// BuildReview reviews the one plugin the checkout holds for ref. Each skill passes the same
// write-boundary validation as a single skill install, and each connector is staged as it
// would be saved, or is skipped with its reason. The digest binds the review: the install
// recomputes it from the pinned commit and refuses on any difference.
func BuildReview(c packs.Checkout, ref packs.Ref, taken Taken, blocklist []string, bodyCapBytes int) (Review, error) {
	if taken.Skill == nil || taken.Server == nil {
		return Review{}, errors.New("plugin review: no name lookups configured")
	}
	pack, err := onePack(c, ref)
	if err != nil {
		return Review{}, err
	}
	if err := packs.ValidPluginName(pack.Name); err != nil {
		return Review{}, fmt.Errorf("plugin name: %w", err)
	}
	root := filepath.Join(c.Root, filepath.FromSlash(pack.Directory))
	if err := checkTree(root); err != nil {
		return Review{}, fmt.Errorf("plugin %s cannot be kept: %w", pack.Name, err)
	}
	r := Review{
		Ref:         ref.String(),
		Commit:      c.Commit,
		Name:        pack.Name,
		Version:     pack.Version,
		Author:      pack.Author,
		Format:      pack.Format,
		Notes:       pack.Notes,
		MCPDisabled: pack.MCPDisabled,
	}
	r.Skills, r.SkippedSkills = reviewSkills(root, pack, taken, blocklist, bodyCapBytes)
	r.Connectors, r.SkippedConnectors = reviewConnectors(pack, ref, taken)
	if r.Digest, err = r.digest(); err != nil {
		return Review{}, err
	}
	return r, nil
}

// onePack picks the plugin the ref names. A bare owner/repo names the repository root, which
// is the common Agent Plugins layout; a marketplace of several plugins needs one named.
func onePack(c packs.Checkout, ref packs.Ref) (packs.Pack, error) {
	switch len(c.Packs) {
	case 1:
		return c.Packs[0], nil
	case 0:
		return packs.Pack{}, invalid(fmt.Errorf("%s: no plugin found", ref))
	}
	names := make([]string, 0, len(c.Packs))
	for _, p := range c.Packs {
		names = append(names, p.Directory)
	}
	return packs.Pack{}, invalid(fmt.Errorf("%s holds %d plugins (%s): name one as %s/<plugin>",
		ref, len(c.Packs), strings.Join(names, ", "), ref.Source))
}

func reviewSkills(root string, pack packs.Pack, taken Taken, blocklist []string, bodyCapBytes int) ([]SkillEntry, []packs.Skipped) {
	kept := []SkillEntry{}
	skipped := slices.Clone(pack.SkippedSkills)
	for _, name := range pack.Skills {
		if taken.Skill(name) {
			skipped = append(skipped, packs.Skipped{Name: name, Reason: "a skill with this name already exists (active or archived)"})
			continue
		}
		dir := filepath.Join(root, "skills", name)
		if _, _, err := skills.ValidateDir(dir, blocklist, bodyCapBytes); err != nil {
			skipped = append(skipped, packs.Skipped{Name: name, Reason: err.Error()})
			continue
		}
		hash, err := skills.HashSkillDir(dir)
		if err != nil {
			skipped = append(skipped, packs.Skipped{Name: name, Reason: err.Error()})
			continue
		}
		kept = append(kept, SkillEntry{Name: name, Hash: hash})
	}
	slices.SortFunc(kept, func(a, b SkillEntry) int { return strings.Compare(a.Name, b.Name) })
	return kept, sortedSkips(skipped)
}

func reviewConnectors(pack packs.Pack, ref packs.Ref, taken Taken) ([]Connector, []packs.Skipped) {
	kept := []Connector{}
	skipped := slices.Clone(pack.SkippedServers)
	for _, s := range pack.Servers {
		if source, ok := taken.Server(s.Name); ok {
			skipped = append(skipped, packs.Skipped{Name: s.Name, Reason: takenReason(source)})
			continue
		}
		server, err := StageServer(s, pack.Name, ref, mcp.ManagedTrust{})
		if err != nil {
			skipped = append(skipped, packs.Skipped{Name: s.Name, Reason: err.Error()})
			continue
		}
		_, class, err := mcp.Classify(server)
		if err != nil {
			skipped = append(skipped, packs.Skipped{Name: s.Name, Reason: err.Error()})
			continue
		}
		c := Connector{Name: s.Name, Transport: server.Type, Placement: PlacementBox, Trust: class, OAuth: s.OAuth, server: server}
		if server.Type == mcp.ServerTypeStreamableHTTP {
			c.Placement, c.OAuth = PlacementRemote, true
		}
		kept = append(kept, c)
	}
	slices.SortFunc(kept, func(a, b Connector) int { return strings.Compare(a.Name, b.Name) })
	return kept, sortedSkips(skipped)
}

// takenReason names what holds a connector's name. A catalog recipe is reserved outright: its
// default-on server has no registry row, and a plugin row of that name would stop it from
// being injected and receive every identity's calls in its place.
func takenReason(source string) string {
	if strings.HasPrefix(source, "recipe:") {
		return "reserved by " + source
	}
	return fmt.Sprintf("name is taken by a server from %q", source)
}

func sortedSkips(s []packs.Skipped) []packs.Skipped {
	if s == nil {
		return []packs.Skipped{}
	}
	slices.SortFunc(s, func(a, b packs.Skipped) int { return strings.Compare(a.Name, b.Name) })
	return s
}

// digest is the sha256 of the review with its own digest blank, and of each staged connector
// with its trust blank: the approval that the install records is not part of what was reviewed.
func (r Review) digest() (string, error) {
	r.Digest = ""
	r.Connectors = slices.Clone(r.Connectors)
	slices.SortFunc(r.Connectors, func(a, b Connector) int { return strings.Compare(a.Name, b.Name) })
	servers := make([]mcp.ManagedServer, 0, len(r.Connectors))
	for _, c := range r.Connectors {
		s := c.server
		s.Trust = mcp.ManagedTrust{}
		servers = append(servers, s)
	}
	raw, err := json.Marshal(struct {
		Review  Review
		Servers []mcp.ManagedServer
	}{r, servers})
	if err != nil {
		return "", fmt.Errorf("plugin review digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
