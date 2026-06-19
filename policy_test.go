package starbox_test

// A4 load-gate (STAR-5 / BOX-04) tests:
//   - TestPolicyNamesAllowlist     explicit Names allowlist; non-permitted builtin -> withheld
//   - TestPolicyCapabilityWidening Capabilities widen to builtins whose caps are a subset
//   - TestPolicyCustomAndDynamic   custom denied -> absent; dynamic denied -> loader NOT invoked
//   - TestPolicyClone              grants are deep-copied (caller cannot mutate after construction)
//   - TestPolicyZeroDeniesAll      the zero Policy permits nothing - not even pure builtins
//   - TestPolicyGatesScriptModule  AddModuleScript modules go through the load gate too
//   - TestPolicySurfaceCheckConverge DescribeSurface/Check report only policy-permitted modules

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

func TestPolicyZeroDeniesAll(t *testing.T) {
	// The zero Policy (empty ModuleAllow: nil Names, Capabilities == CapPure == 0)
	// must permit NOTHING - not even a pure builtin. Regression guard for the
	// zero-value bug where CapPure widened to every pure module because
	// CapPure.Has(pure) is true.
	b := starbox.NewWithPolicy("zero", starbox.Policy{})
	b.SetPrintFunc(noopPrint)
	b.SetModuleSet(starbox.FullModuleSet) // requests everything; the policy permits none

	// A pure builtin (math) is requested by the set but withheld by the policy.
	var mwe starbox.ModuleWithheldError
	if _, err := b.Run(`load("math", "pi")`); !errors.As(err, &mwe) {
		t.Errorf("zero policy must withhold even pure builtins; got %T: %v", err, err)
	}
}

func TestPolicyGatesScriptModule(t *testing.T) {
	// A host-injected script module is subject to the load gate too: a zero
	// Policy must NOT let a script load() it (P0-1).
	t.Run("zero policy denies", func(t *testing.T) {
		b := starbox.NewWithPolicy("audit", starbox.Policy{})
		b.SetPrintFunc(noopPrint)
		b.AddModuleScript("secret", `x = 42`)
		if out, err := b.Run(`load("secret.star", "x")` + "\n" + `y = x`); err == nil {
			t.Errorf("zero policy must not allow a script module; got out=%v", out)
		}
	})
	// Permitting the script module by its registered (.star) name lets it load.
	t.Run("explicit name allows", func(t *testing.T) {
		b := starbox.NewWithPolicy("audit", starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"secret.star"}}})
		b.SetPrintFunc(noopPrint)
		b.AddModuleScript("secret", `x = 42`)
		out, err := b.Run(`load("secret.star", "x")` + "\n" + `y = x`)
		if err != nil || out["y"] != int64(42) {
			t.Errorf("explicit allow should load; out=%v err=%v", out, err)
		}
	})
	// Nested-path script modules are gated by their full .star key.
	t.Run("nested explicit allows", func(t *testing.T) {
		b := starbox.NewWithPolicy("audit", starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"lib/util.star"}}})
		b.SetPrintFunc(noopPrint)
		b.AddModuleScript("lib/util", `v = 7`)
		out, err := b.Run(`load("lib/util.star", "v")` + "\n" + `w = v`)
		if err != nil || out["w"] != int64(7) {
			t.Errorf("nested explicit allow should load; out=%v err=%v", out, err)
		}
	})
}

func TestPolicySurfaceCheckConverge(t *testing.T) {
	// Policy permits only math; the Box otherwise requests everything.
	mk := func() *starbox.Starbox {
		b := starbox.NewWithPolicy("audit", starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"math"}}})
		b.SetPrintFunc(noopPrint)
		b.SetModuleSet(starbox.FullModuleSet)
		return b
	}

	// DescribeSurface must report only policy-permitted modules (P0-2).
	sf, err := mk().DescribeSurface()
	if err != nil {
		t.Fatalf("DescribeSurface: %v", err)
	}
	names := map[string]bool{}
	for _, m := range sf.Modules {
		names[m.Name] = true
	}
	if !names["math"] {
		t.Error("math should be in the policy-filtered surface")
	}
	if names["file"] || names["re"] {
		t.Errorf("withheld modules leaked into surface: %v", names)
	}

	// Check must flag a policy-withheld module name as undefined.
	if diags, err := mk().Check(`x = file`); err != nil {
		t.Fatalf("Check: %v", err)
	} else if len(diags) == 0 {
		t.Error("Check should flag a policy-withheld name (file) as undefined")
	}
	// A permitted module resolves cleanly.
	if diags, err := mk().Check(`y = math.pi`); err != nil || len(diags) != 0 {
		t.Errorf("permitted module should pass Check; diags=%v err=%v", diags, err)
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
