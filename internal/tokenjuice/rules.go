package tokenjuice

import "sync"

// LoadOptions configures which rule layers to load.
type LoadOptions struct {
	Builtin bool   // when true, include the embedded builtin rules
	UserDir string // path to a user-local rule directory; "" = skip
}

var (
	loadOnce      sync.Once
	cachedBuiltin []*CompiledRule
)

// LoadBuiltinRules returns the compiled builtin rule set, loaded once via
// sync.Once. Returns an empty slice in the skeleton; rules are vendored in US-TJ04.
func LoadBuiltinRules() []*CompiledRule {
	loadOnce.Do(func() {
		cachedBuiltin = []*CompiledRule{}
	})
	return cachedBuiltin
}

// LoadRules loads rules according to opts.
func LoadRules(opts LoadOptions) ([]*CompiledRule, error) {
	if opts.Builtin {
		return LoadBuiltinRules(), nil
	}
	return []*CompiledRule{}, nil
}
