package starbox_test

// A4 load-gate (STAR-5 / BOX-04) tests:
//   - TestPolicyNamesAllowlist     explicit Names allowlist; non-permitted builtin -> withheld
//   - TestPolicyCapabilityWidening Capabilities widen to builtins whose caps are a subset
//   - TestPolicyCustomAndDynamic   custom denied -> absent; dynamic denied -> loader NOT invoked
//   - TestPolicyClone              grants are deep-copied (caller cannot mutate after construction)

import (
	"errors"
	"testing"

	"github.com/1set/starbox"
	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
)

func TestPolicyNamesAllowlist(t *testing.T) {
	mk := func() *starbox.Starbox {
		b := starbox.NewWithPolicy("pol", starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"math", "json"}}})
		b.SetPrintFunc(noopPrint)
		b.SetModuleSet(starbox.FullModuleSet) // requests everything; policy tightens to {math,json}
		return b
	}
	// Permitted builtins load and are usable.
	if _, err := mk().Run(hereDoc("a = math.pi\nb = json.encode({})")); err != nil {
		t.Errorf("permitted modules should load: %v", err)
	}
	// A builtin present in Full but NOT permitted is withheld at load().
	var mwe starbox.ModuleWithheldError
	if _, err := mk().Run(`load("re", "x")`); !errors.As(err, &mwe) {
		t.Errorf("denied builtin: want ModuleWithheldError, got %T: %v", err, err)
	}
}

func TestPolicyCapabilityWidening(t *testing.T) {
	mk := func() *starbox.Starbox {
		// CapNetwork permits builtins needing at most network caps (pure + network).
		b := starbox.NewWithPolicy("pol", starbox.Policy{Modules: starbox.ModuleAllow{Capabilities: starlet.CapNetwork}})
		b.SetPrintFunc(noopPrint)
		b.SetModuleSet(starbox.FullModuleSet)
		return b
	}
	// A pure builtin is permitted (pure ⊆ network).
	if _, err := mk().Run(hereDoc("a = math.sqrt(4.0)")); err != nil {
		t.Errorf("pure module should load under CapNetwork: %v", err)
	}
	// A filesystem builtin is withheld (CapFileSystem ⊄ CapNetwork).
	var mwe starbox.ModuleWithheldError
	if _, err := mk().Run(`load("file", "x")`); !errors.As(err, &mwe) {
		t.Errorf("filesystem module under CapNetwork: want withheld, got %T: %v", err, err)
	}
}

func TestPolicyCustomAndDynamic(t *testing.T) {
	withMods := func(name string) *starbox.Starbox {
		b := starbox.NewWithPolicy(name, starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"okmod"}}})
		b.SetPrintFunc(noopPrint)
		b.AddModuleData("okmod", starlark.StringDict{"v": starlark.MakeInt(1)})
		b.AddModuleData("denymod", starlark.StringDict{"v": starlark.MakeInt(2)})
		return b
	}
	// Permitted custom module loads.
	if out, err := withMods("c1").Run(hereDoc("x = okmod.v")); err != nil || out["x"] != int64(1) {
		t.Errorf("permitted custom module: out=%v err=%v", out, err)
	}
	// Denied custom module is absent -> referencing it fails.
	if _, err := withMods("c2").Run(hereDoc("y = denymod.v")); err == nil {
		t.Error("denied custom module should not be loadable")
	}

	// Dynamic: a denied name must NOT invoke the loader (fail-closed; review #5).
	called := map[string]bool{}
	b := starbox.NewWithPolicy("dyn", starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"dynok"}}})
	b.SetPrintFunc(noopPrint)
	b.AddNamedModules("dynok", "dyndeny")
	b.SetDynamicModuleLoader(func(name string) (starlet.ModuleLoader, error) {
		called[name] = true
		return dataconv.WrapModuleData(name, starlark.StringDict{"v": starlark.MakeInt(7)}), nil
	})
	if out, err := b.Run(hereDoc("z = dynok.v")); err != nil || out["z"] != int64(7) {
		t.Errorf("permitted dynamic module: out=%v err=%v", out, err)
	}
	if called["dyndeny"] {
		t.Error("denied dynamic module's loader was invoked (must be skipped before metaLoad)")
	}
	if !called["dynok"] {
		t.Error("permitted dynamic module's loader was not invoked")
	}
}

func TestPolicyClone(t *testing.T) {
	names := []string{"math"}
	b := starbox.NewWithPolicy("clone", starbox.Policy{Modules: starbox.ModuleAllow{Names: names}})
	b.SetPrintFunc(noopPrint)
	b.SetModuleSet(starbox.FullModuleSet)

	// Mutating the caller's slice after construction must not change grants.
	names[0] = "json"
	if _, err := b.Run(hereDoc("a = math.pi")); err != nil {
		t.Errorf("clone should preserve the original grant (math): %v", err)
	}
}
