package starbox

import (
	"reflect"
	"sort"
	"testing"

	"github.com/1set/starlet"
)

// TestModuleSetsGolden pins the predefined module sets so that upgrading
// starlet can never silently change what a script in a given set may load.
//
// This is the BOX-05 guard. The Safe set used to be defined by subtraction
// from starlet's full module set, so a new starlet module (e.g. net) slipped
// into Safe the moment the dependency was bumped. The sets are now explicit
// allowlists, and this test:
//  1. pins the exact contents of every predefined set;
//  2. proves, via starlet's own capability classification, that no Safe
//     member can reach the network or filesystem (and no Network member the
//     filesystem); and
//  3. fails closed - every starlet builtin module must be consciously
//     classified, so a newly added one trips this test instead of silently
//     entering Safe.
func TestModuleSetsGolden(t *testing.T) {
	sorted := func(ss []string) []string {
		out := append([]string(nil), ss...)
		sort.Strings(out)
		return out
	}

	// 1. Exact contents of each predefined set.
	wantSafe := []string{
		"atom", "base64", "csv", "go_idiomatic", "hashlib", "json", "math",
		"random", "re", "regex", "serial", "stats", "string", "struct", "time",
	}
	wantNetwork := []string{
		"atom", "base64", "csv", "go_idiomatic", "hashlib", "http", "json",
		"log", "math", "net", "random", "re", "regex", "serial", "stats",
		"string", "struct", "time",
	}
	for _, tc := range []struct {
		name string
		set  ModuleSetName
		want []string
	}{
		{"empty", EmptyModuleSet, nil},
		{"safe", SafeModuleSet, wantSafe},
		{"network", NetworkModuleSet, wantNetwork},
		{"full", FullModuleSet, starlet.GetAllBuiltinModuleNames()},
	} {
		if got, want := sorted(moduleSets[tc.set]), sorted(tc.want); !reflect.DeepEqual(got, want) {
			t.Errorf("%s set: want %v, got %v", tc.name, want, got)
		}
	}

	// 2. Security invariant cross-checked against starlet's capability map:
	// Safe must contain no network/filesystem module; Network no filesystem
	// module. go_idiomatic carries CapLog|CapProcess and is a deliberate Safe
	// member, so the line is drawn at network/filesystem, not "pure".
	assertNoCap := func(set ModuleSetName, forbidden starlet.ModuleCapability) {
		for _, m := range moduleSets[set] {
			mc, ok := starlet.GetBuiltinModuleCapability(m)
			if !ok {
				t.Errorf("%s: module %q has no starlet capability classification", set, m)
				continue
			}
			if mc.Intersects(forbidden) {
				t.Errorf("%s: module %q carries forbidden capability %v", set, m, mc)
			}
		}
	}
	assertNoCap(SafeModuleSet, starlet.CapNetwork|starlet.CapFileSystem)
	assertNoCap(NetworkModuleSet, starlet.CapFileSystem)

	// 3. Fail-closed coverage: every starlet builtin module must be accounted
	// for by an explicit classification - Safe, the network extras, or the
	// full-only filesystem/process modules. A new (or removed) starlet module
	// makes the union differ from the full set and trips this, forcing a
	// conscious decision instead of a silent change to Safe.
	fullOnly := []string{"file", "path", "runtime"}
	classified := append(append(append([]string(nil), safeModuleNames...), networkExtraNames...), fullOnly...)
	if got, want := sorted(classified), sorted(fullModuleNames); !reflect.DeepEqual(got, want) {
		t.Errorf("module classification drifted from starlet's full set:\n  classified: %v\n  full:       %v\n(classify the new/removed starlet module in module.go)", got, want)
	}
}

// TestBuiltinModuleMembers covers the member-enumeration helper across the
// three loader shapes Starlet returns, plus the unknown-module path. It backs
// DescribeSurface (STAR-9 / BOX-09).
func TestBuiltinModuleMembers(t *testing.T) {
	has := func(ss []string, want string) bool {
		for _, s := range ss {
			if s == want {
				return true
			}
		}
		return false
	}
	// Module shape: members come from the module value's attributes.
	if m := builtinModuleMembers("math"); !has(m, "sqrt") || !has(m, "pi") {
		t.Errorf("math members: %v", m)
	}
	// Flat shape: the top-level binding names (go_idiomatic).
	if m := builtinModuleMembers("go_idiomatic"); !has(m, "sleep") || !has(m, "exit") {
		t.Errorf("go_idiomatic members: %v", m)
	}
	// Single bare builtin: the binding name itself.
	if m := builtinModuleMembers("struct"); !reflect.DeepEqual(m, []string{"struct"}) {
		t.Errorf("struct members: want [struct], got %v", m)
	}
	// Unknown module: no loader, so nil members.
	if m := builtinModuleMembers("nonexistent_xyz"); m != nil {
		t.Errorf("unknown module members: want nil, got %v", m)
	}
}
