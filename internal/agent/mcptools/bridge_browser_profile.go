package mcptools

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// browserDefaultSession is the session agent-browser drives when a call names none.
const browserDefaultSession = "default"

var errBrowserExtraArgs = errors.New("browser extraArgs must be a list of strings")

// withBrowserProfile gives an agent-browser call the Chrome profile of its session, a directory
// that outlives the browser and the box, which is what keeps a login (prd.md §12, a persistent
// profile per browser session). agent-browser hashes launch options per command and relaunches
// on a temporary profile when one call omits --profile, so every call carries it; and `agent-browser
// mcp` runs each call as its own child, past the box's entry point, so it rides on extraArgs.
// agent-browser expands "~" against the HOME that entry point sets. The session is written back
// so the profile and the session a call drives can never differ.
func withBrowserProfile(args map[string]any) (map[string]any, error) {
	session := browserDefaultSession
	if raw := args["session"]; raw != nil {
		name, ok := raw.(string)
		if !ok || !mcpmanager.ValidBrowserSession(name) {
			return nil, fmt.Errorf("browser session %q: use 1-48 letters, digits, '-' or '_'", fmt.Sprint(raw))
		}
		session = name
	}
	extra, err := browserExtraArgs(args["extraArgs"])
	if err != nil {
		return nil, err
	}
	out := maps.Clone(args)
	if out == nil {
		out = map[string]any{}
	}
	out["session"] = session
	out["extraArgs"] = append(extra, "--profile", "~/profiles/"+session)
	return out, nil
}

func browserExtraArgs(raw any) ([]any, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errBrowserExtraArgs
	}
	for _, arg := range list {
		flag, ok := arg.(string)
		if !ok {
			return nil, errBrowserExtraArgs
		}
		if flag == "--profile" || strings.HasPrefix(flag, "--profile=") {
			return nil, errors.New("browser extraArgs: Aura keeps one profile per session; name the session instead of passing --profile")
		}
	}
	return slices.Clone(list), nil
}
