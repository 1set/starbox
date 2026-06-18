package starbox

import (
	"errors"
	"fmt"

	"github.com/1set/starlet"
	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Diagnostic is one problem Check found in a script - a syntax error or a
// resolve error (e.g. an undefined name) - with its 1-based source position.
type Diagnostic struct {
	Msg  string
	Line int
	Col  int
}

// String renders the diagnostic as "line:col: message".
func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d: %s", d.Line, d.Col, d.Msg)
}

// Check parses and resolves a script against the Box's configured environment
// WITHOUT executing it, returning the problems found (syntax errors, undefined
// names). A nil result means the script compiles cleanly against this Box.
//
// Check is side-effect free: it inspects only the configured names and the
// known-pure builtin module loaders to learn which globals exist; it never runs
// the script and never invokes an opaque (AddModuleLoader) custom loader. It
// catches resolve-time problems only - load() of a missing or withheld module
// is a run-time concern (see the R7 withheld-module work), not a resolve error.
func (s *Starbox) Check(script string) ([]Diagnostic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pre, err := s.predeclaredNames()
	if err != nil {
		return nil, err
	}
	// Mirror the dialect newStarMachine configures (Set + global reassign), so
	// Check never flags a dialect feature that a real Run would accept.
	opts := &syntax.FileOptions{Set: true, GlobalReassign: true, TopLevelControl: true, While: true}
	_, _, perr := starlark.SourceProgramOptions(opts, "box.star", []byte(script), func(name string) bool { return pre[name] })
	if perr == nil {
		return nil, nil
	}
	return toDiagnostics(perr), nil
}

// predeclaredNames computes the set of global names a script may reference in
// this Box, so the resolver does not flag them as undefined: configured
// globals, the top-level names each builtin module injects, custom module
// names, and __modules__. The caller holds s.mu. Side-effect free with respect
// to opaque custom loaders.
func (s *Starbox) predeclaredNames() (map[string]bool, error) {
	pre := map[string]bool{"__modules__": true}
	for k := range s.globals {
		pre[k] = true
	}

	setNames, err := getModuleSet(s.modSet)
	if err != nil {
		return nil, err
	}
	addBuiltin := func(name string) {
		for _, g := range builtinModuleGlobalNames(name) {
			pre[g] = true
		}
	}
	for _, n := range setNames {
		addBuiltin(n)
	}
	for _, n := range intersectStrings(fullModuleNames, s.namedMods) {
		addBuiltin(n)
	}
	// Custom modules are preloaded under their registered name.
	for name := range s.loadMods {
		pre[name] = true
	}
	return pre, nil
}

// builtinModuleGlobalNames returns the top-level names a builtin module injects
// into the global namespace: the module name for a namespaced module (math ->
// "math") or each flat binding for a flat module (go_idiomatic -> sleep, exit,
// ...). It never panics; a misbehaving loader degrades to nil.
func builtinModuleGlobalNames(name string) (names []string) {
	defer func() {
		if recover() != nil {
			names = nil
		}
	}()
	loader := starlet.GetBuiltinModule(name)
	if loader == nil {
		return nil
	}
	sd, err := loader()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(sd))
	for k := range sd {
		out = append(out, k)
	}
	return out
}

// toDiagnostics converts the error from SourceProgramOptions (a resolve error
// list or a single syntax error) into Diagnostics.
func toDiagnostics(err error) []Diagnostic {
	var rl resolve.ErrorList
	if errors.As(err, &rl) {
		ds := make([]Diagnostic, 0, len(rl))
		for _, e := range rl {
			ds = append(ds, Diagnostic{Msg: e.Msg, Line: int(e.Pos.Line), Col: int(e.Pos.Col)})
		}
		return ds
	}
	var se syntax.Error
	if errors.As(err, &se) {
		return []Diagnostic{{Msg: se.Msg, Line: int(se.Pos.Line), Col: int(se.Pos.Col)}}
	}
	return []Diagnostic{{Msg: err.Error()}}
}
