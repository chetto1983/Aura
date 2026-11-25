package main

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"

	"golang.org/x/tools/go/packages"
)

// finding is one exported function or method of the main module that no non-test code
// references and that no interface method in the program could reach.
type finding struct {
	// Symbol is <package path>.<Func> or <package path>.<Type>.<Method>, the same
	// spelling deadcode prints, so one allowlist serves both tools.
	Symbol string
	// Pos is file:line, relative to dir when it can be.
	Pos string
}

// analyze reports the main module's exported functions and methods that nothing in pkgs
// references. pkgs must be loaded without tests, so a reference from a _test.go file does
// not count. A method is spared when some named type of the main module, through its own
// method set (embedding included), implements an interface in the program that declares
// it: a call through that interface is invisible to a reference count. Main packages,
// generated files and unexported identifiers are out of scope (deadcode and unused cover
// them).
func analyze(pkgs []*packages.Package, dir string) []finding {
	var all []*packages.Package
	packages.Visit(pkgs, nil, func(p *packages.Package) { all = append(all, p) })

	used := map[*types.Func]bool{}
	var ifaces []*types.Interface
	var named []types.Type
	var candidates []*types.Func
	generated := map[string]bool{}
	for _, p := range all {
		if p.Types == nil {
			continue
		}
		ifaces = append(ifaces, scopeInterfaces(p.Types.Scope())...)
		main := p.Module != nil && p.Module.Main
		if p.TypesInfo != nil {
			for _, tv := range p.TypesInfo.Types {
				if it, ok := tv.Type.Underlying().(*types.Interface); ok {
					ifaces = append(ifaces, it)
				}
				if nt, ok := tv.Type.(*types.Named); ok && main && nt.TypeArgs().Len() > 0 {
					named = append(named, nt, types.NewPointer(nt))
				}
			}
			for _, obj := range p.TypesInfo.Uses {
				if f, ok := obj.(*types.Func); ok {
					used[f.Origin()] = true
				}
			}
		}
		if !main || p.Name == "main" {
			continue
		}
		for _, file := range p.Syntax {
			if ast.IsGenerated(file) {
				generated[p.Fset.File(file.Pos()).Name()] = true
			}
		}
		named = append(named, scopeConcreteTypes(p.Types.Scope())...)
		candidates = append(candidates, exportedFuncs(p.Types.Scope())...)
	}

	spared := reachableThroughInterfaces(named, ifaces, candidates)
	var out []finding
	for _, f := range candidates {
		if used[f] || spared[f] {
			continue
		}
		pos := all[0].Fset.Position(f.Pos())
		if generated[pos.Filename] {
			continue
		}
		out = append(out, finding{Symbol: symbolOf(f), Pos: relPos(dir, pos.Filename, pos.Line)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

func scopeInterfaces(scope *types.Scope) []*types.Interface {
	var out []*types.Interface
	for _, name := range scope.Names() {
		if tn, ok := scope.Lookup(name).(*types.TypeName); ok {
			if it, ok := tn.Type().Underlying().(*types.Interface); ok {
				out = append(out, it)
			}
		}
	}
	return out
}

// scopeConcreteTypes returns every non-generic named non-interface type and its pointer.
// Generic types are taken from their instantiations instead (analyze), since only an
// instance has a method set an interface can be checked against.
func scopeConcreteTypes(scope *types.Scope) []types.Type {
	var out []types.Type
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		nt, ok := tn.Type().(*types.Named)
		if !ok || nt.TypeParams().Len() > 0 {
			continue
		}
		if _, isIface := nt.Underlying().(*types.Interface); !isIface {
			out = append(out, nt, types.NewPointer(nt))
		}
	}
	return out
}

func exportedFuncs(scope *types.Scope) []*types.Func {
	var out []*types.Func
	for _, name := range scope.Names() {
		switch obj := scope.Lookup(name).(type) {
		case *types.Func:
			if obj.Exported() {
				out = append(out, obj)
			}
		case *types.TypeName:
			nt, ok := obj.Type().(*types.Named)
			if !ok || obj.IsAlias() {
				continue
			}
			for i := range nt.NumMethods() {
				if m := nt.Method(i); m.Exported() {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// reachableThroughInterfaces marks every candidate method that some type's method set
// carries under the name of a method of an interface that type implements.
func reachableThroughInterfaces(named []types.Type, ifaces []*types.Interface, candidates []*types.Func) map[*types.Func]bool {
	isCandidate := make(map[*types.Func]bool, len(candidates))
	for _, f := range candidates {
		isCandidate[f] = true
	}
	spared := map[*types.Func]bool{}
	for _, t := range named {
		mset := types.NewMethodSet(t)
		byName := map[string]*types.Func{}
		for i := range mset.Len() {
			if f, ok := mset.At(i).Obj().(*types.Func); ok && isCandidate[f.Origin()] {
				byName[f.Name()] = f.Origin()
			}
		}
		if len(byName) == 0 {
			continue
		}
		for _, it := range ifaces {
			var hits []*types.Func
			for i := range it.NumMethods() {
				if f, ok := byName[it.Method(i).Name()]; ok {
					hits = append(hits, f)
				}
			}
			if len(hits) > 0 && types.Implements(t, it) {
				for _, f := range hits {
					spared[f] = true
				}
			}
		}
	}
	return spared
}

func symbolOf(f *types.Func) string {
	name := f.Name()
	if recv := f.Signature().Recv(); recv != nil {
		t := recv.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if nt, ok := t.(*types.Named); ok {
			name = nt.Obj().Name() + "." + name
		}
	}
	return f.Pkg().Path() + "." + name
}

func relPos(dir, file string, line int) string {
	if rel, err := filepath.Rel(dir, file); err == nil {
		file = rel
	}
	return file + ":" + strconv.Itoa(line)
}
