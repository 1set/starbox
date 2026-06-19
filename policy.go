package starbox

import "github.com/1set/starlet"

// Policy is the host-side, default-deny capability grant for a Box. It is
// declared in Go (never in Starlark), cannot be read or mutated by the
// sandboxed script, and is applied as an ADDITIVE opt-in via NewWithPolicy —
// a Box created with New (no policy) is unaffected.
//
// A4 scope note: this carries only the LOAD gate (which modules a script may
// load). The exec gate (what a loaded module may DO: fs/net/cmd/secret) is NOT
// here — starbox cannot reach starpkg modules' construction-time knobs (it
// never imports starpkg; they arrive as opaque loaders), so exec-gating lives
// where the loaders are constructed (the host shell / starcli). Shipping inert
// fs/net grant fields here would be a fail-open footgun, so they are omitted.
type Policy struct {
	// Modules is the load gate: which module names a script may load.
	Modules ModuleAllow
}

// ModuleAllow is an explicit load-gate allowlist (never a denylist subtraction —
// that was BOX-05). A module name is permitted iff it is listed in Names OR it
// is a builtin whose capability set is a subset of Capabilities. The effective
// loadable set is whatever the Box requests (module set + AddNamedModules +
// custom/dynamic) INTERSECTED with what this allows — the policy only tightens.
//
// The zero ModuleAllow (nil Names, Capabilities = CapPure = 0) permits NOTHING —
// strict default-deny by construction.
type ModuleAllow struct {
	// Names are module names permitted verbatim (builtin or custom/dynamic).
	Names []string
	// Capabilities is OPT-IN capability widening: a non-zero tier permits every
	// builtin whose capability bits are a subset of it (e.g. CapNetwork permits
	// pure + network builtins). The zero value (CapPure) does NOT widen — an
	// exact pure allowlist is expressed via Names or a module set.
	Capabilities starlet.ModuleCapability
}

// allows reports whether the policy permits loading the module named name.
func (p *Policy) allows(name string) bool {
	for _, n := range p.Modules.Names {
		if n == name {
			return true
		}
	}
	// Capability widening is opt-in: a non-zero tier permits builtins whose
	// capability bits are a subset of it. The zero value (CapPure) does NOT
	// widen — Names is the exact allowlist. Custom/dynamic modules carry no
	// starlet capability, so they must be named explicitly.
	if c := p.Modules.Capabilities; c != starlet.CapPure {
		if mc, ok := starlet.GetBuiltinModuleCapability(name); ok {
			return c.Has(mc)
		}
	}
	return false
}

// clone deep-copies the policy so a caller cannot mutate grants after
// construction.
func (p Policy) clone() Policy {
	names := append([]string(nil), p.Modules.Names...)
	return Policy{Modules: ModuleAllow{Names: names, Capabilities: p.Modules.Capabilities}}
}

// policyAllows reports whether name may load given the Box's policy (always true
// when no policy is set, preserving New + SetModuleSet behaviour unchanged).
func (s *Starbox) policyAllows(name string) bool {
	return s.policy == nil || s.policy.allows(name)
}

// gateNames returns only the policy-permitted names (the input unchanged when no
// policy is set).
func (s *Starbox) gateNames(names []string) []string {
	if s.policy == nil {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if s.policy.allows(n) {
			out = append(out, n)
		}
	}
	return out
}
