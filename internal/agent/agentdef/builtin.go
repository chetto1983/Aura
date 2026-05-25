package agentdef

import (
	"context"
	"embed"
	"fmt"
	"sync"
)

//go:embed builtin/*/agent.json builtin/*/prompt.md
var BuiltinFS embed.FS

var (
	builtinOnce sync.Once
	builtinReg  *Registry
	builtinErr  error
)

func BuiltinRegistry() (*Registry, error) {
	builtinOnce.Do(func() {
		builtinReg, builtinErr = NewRegistry(context.Background(), &Loader{BuiltinFS: BuiltinFS}, &Validator{})
	})
	return builtinReg, builtinErr
}

func BuiltinDefinition(id string) (AgentDefinition, error) {
	reg, err := BuiltinRegistry()
	if err != nil {
		return AgentDefinition{}, err
	}
	def, ok := reg.Get(id)
	if !ok {
		return AgentDefinition{}, fmt.Errorf("agentdef: builtin %q not found", id)
	}
	return def, nil
}

func MustBuiltinPrompt(id string) string {
	def, err := BuiltinDefinition(id)
	if err != nil {
		panic(err)
	}
	return def.Prompt
}
