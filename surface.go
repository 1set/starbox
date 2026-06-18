package starbox

import (
	"fmt"
	"sort"

	"github.com/1set/starlet"
	"go.starlark.net/starlark"
)

// ModuleOrigin identifies where a module in a Box's Surface comes from.
type ModuleOrigin string

const (
	// OriginBuiltin is a Starlet builtin module (from a module set or AddNamedModules).
	OriginBuiltin ModuleOrigin = "builtin"
	// OriginCustom is a module added via AddModuleLoader/AddModule{Functions,Data}/AddStruct{Functions,Data}.
	OriginCustom ModuleOrigin = "custom"
	// OriginScript is a *.star module added via AddModuleScript.
	OriginScript ModuleOrigin = "script"
	// OriginDynamic is a module resolved on demand by a DynamicModuleLoader.
	OriginDynamic ModuleOrigin = "dynamic"
)

// ModuleSurface describes one module a Box exposes to scripts.
type ModuleSurface struct {
	// Name is the name used in load("name", ...) or as a global.
	Name string
	// Origin says where the module comes from.
	Origin ModuleOrigin
	// Members are the exported member names, sorted. It is nil (not empty) when
	// the members cannot be enumerated without running host code or the script:
	// an opaque AddModuleLoader func, a script (.star) module, or a dynamic one.
	Members []string
}

// GlobalSurface describes one global value injected into a Box.
type GlobalSurface struct {
	// Name is the binding's name in the script namespace.
	Name string
	// Type is its Starlark type (e.g. "string", "builtin_function_or_method"),
	// or the Go type for a host value that has not been converted yet.
	Type string
}

// Surface is the inventory of everything a Box exposes to a script, derived
// from the Box's configuration WITHOUT running the script. It is the
// authoritative answer to "what can this script see", so callers need no
// hand-maintained registry that can drift from the real configuration.
//
// DescribeSurface is side-effect free: it reads configuration and inspects the
// known-pure builtin module loaders, but it never invokes an opaque
// (AddModuleLoader) loader, never resolves a dynamic module, and never runs the
// user script. Members that would require any of those are reported as nil.
type Surface struct {
	// Modules are the loadable modules, sorted by Name.
	Modules []ModuleSurface
	// Globals are the injected globals/builtins, sorted by Name.
	Globals []GlobalSurface
}

// DescribeSurface enumerates the configured surface of the Box without running
// a script (and without requiring a prior run). See Surface for the contract.
func (s *Starbox) DescribeSurface() (Surface, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	var mods []ModuleSurface
	add := func(name string, origin ModuleOrigin, members []string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		mods = append(mods, ModuleSurface{Name: name, Origin: origin, Members: members})
	}

	// 1. builtin modules from the selected set (highest load precedence).
	setNames, err := getModuleSet(s.modSet)
	if err != nil {
		return Surface{}, err
	}
	for _, n := range setNames {
		add(n, OriginBuiltin, builtinModuleMembers(n))
	}
	// 2. additional builtin modules requested by name.
	for _, n := range intersectStrings(fullModuleNames, s.namedMods) {
		add(n, OriginBuiltin, builtinModuleMembers(n))
	}
	// 3. custom modules. Members were captured at Add*Data/Functions/Struct
	// time; an opaque AddModuleLoader has no captured members (nil).
	for name := range s.loadMods {
		add(name, OriginCustom, s.modMembers[name])
	}
	// 4. script (.star) modules. Members would need a resolve pass - left nil.
	for fp := range s.scriptMods {
		add(fp, OriginScript, nil)
	}
	// 5. names that route to the dynamic loader (only when one is configured).
	if s.dynMods != nil {
		for _, n := range s.namedMods {
			add(n, OriginDynamic, nil)
		}
	}

	sort.Slice(mods, func(i, j int) bool { return mods[i].Name < mods[j].Name })

	// globals: name + type, no side-effectful conversion.
	globals := make([]GlobalSurface, 0, len(s.globals))
	for name, v := range s.globals {
		globals = append(globals, GlobalSurface{Name: name, Type: globalValueType(v)})
	}
	sort.Slice(globals, func(i, j int) bool { return globals[i].Name < globals[j].Name })

	return Surface{Modules: mods, Globals: globals}, nil
}

// loadBuiltinDict returns the StringDict a Starlet builtin module's loader
// produces, or nil if name is not a builtin module or its loader fails. These
// loaders are Starlet's own known-pure constructors, invoked here only for
// config-time introspection (never with script input), so no panic guard is
// needed.
func loadBuiltinDict(name string) starlark.StringDict {
	loader := starlet.GetBuiltinModule(name)
	if loader == nil {
		return nil
	}
	sd, err := loader()
	if err != nil {
		return nil
	}
	return sd
}

// builtinModuleMembers returns the member names a Starlet builtin module
// exposes. It handles the three shapes a loader can return: a single
// module/struct value with attrs, a single bare builtin, or a flat set of
// top-level bindings (e.g. go_idiomatic).
func builtinModuleMembers(name string) []string {
	sd := loadBuiltinDict(name)
	if len(sd) == 0 {
		return nil
	}
	// Module/Struct shape: a single value that has attributes.
	if len(sd) == 1 {
		for _, v := range sd {
			if ha, ok := v.(starlark.HasAttrs); ok {
				out := append([]string(nil), ha.AttrNames()...)
				sort.Strings(out)
				return out
			}
		}
	}
	// Flat shape (or a single bare builtin): the top-level binding names.
	out := make([]string, 0, len(sd))
	for k := range sd {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// globalValueType reports a global's Starlark type, falling back to the Go type
// for a host value that has not been converted to a Starlark value.
func globalValueType(v interface{}) string {
	if v == nil {
		return "NoneType"
	}
	if sv, ok := v.(starlark.Value); ok {
		return sv.Type()
	}
	return fmt.Sprintf("%T", v)
}

// recordModMembers captures the member names of a custom data/struct/function
// module at configuration time, so DescribeSurface can report them without ever
// invoking the (wrapped) loader. The caller holds s.mu.
func (s *Starbox) recordModMembers(name string, sfd starlark.StringDict) {
	if s.modMembers == nil {
		s.modMembers = make(map[string][]string)
	}
	keys := make([]string, 0, len(sfd))
	for k := range sfd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s.modMembers[name] = keys
}
