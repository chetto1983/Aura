package agentdef

import (
	"context"
	"sort"
	"strings"
)

type Registry struct {
	defs map[string]AgentDefinition
	ids  []string
}

func NewRegistry(ctx context.Context, loader *Loader, validator *Validator) (*Registry, error) {
	var defs []AgentDefinition
	var err error
	if loader != nil {
		defs, err = loader.Load(ctx)
		if err != nil {
			return nil, err
		}
	}
	if validator == nil {
		validator = &Validator{}
	}
	if err := validator.Validate(defs); err != nil {
		return nil, err
	}

	reg := &Registry{defs: make(map[string]AgentDefinition, len(defs))}
	for _, def := range defs {
		id := strings.TrimSpace(def.ID)
		reg.defs[id] = cloneDefinition(def)
		reg.ids = append(reg.ids, id)
	}
	sort.Strings(reg.ids)
	return reg, nil
}

func (r *Registry) Get(id string) (AgentDefinition, bool) {
	if r == nil || r.defs == nil {
		return AgentDefinition{}, false
	}
	def, ok := r.defs[strings.TrimSpace(id)]
	if !ok {
		return AgentDefinition{}, false
	}
	return cloneDefinition(def), true
}

func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.ids...)
}

func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.ids)
}
