package starbox_test

// DescribeSurface (STAR-9 / BOX-09 / R3) tests, grouped by concern:
//   - TestDescribeSurfaceBuiltin       builtin set modules + member enumeration
//   - TestDescribeSurfaceCustom        AddModule{Data,Functions}/AddModuleLoader origins + members
//   - TestDescribeSurfaceGlobals       global name + type reporting
//   - TestDescribeSurfaceScriptDynamic script (.star) and dynamic modules report name-only
//   - TestDescribeSurfaceSideEffectFree no script run; configuration and a later Run still work

import (
	"reflect"
	"testing"

	"github.com/1set/starbox"
	"github.com/1set/starlet"
	"go.starlark.net/starlark"
)

// findModule returns the ModuleSurface with the given name, or false.
func findModule(sf starbox.Surface, name string) (starbox.ModuleSurface, bool) {
	for _, m := range sf.Modules {
		if m.Name == name {
			return m, true
		}
	}
	return starbox.ModuleSurface{}, false
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestDescribeSurfaceBuiltin(t *testing.T) {
	b := starbox.New("surface-builtin")
	b.SetModuleSet(starbox.SafeModuleSet)

	sf, err := b.DescribeSurface()
	if err != nil {
		t.Fatalf("DescribeSurface: %v", err)
	}

	// A Module-shape builtin exposes its members.
	if m, ok := findModule(sf, "math"); !ok {
		t.Error("math not in Safe surface")
	} else {
		if m.Origin != starbox.OriginBuiltin {
			t.Errorf("math origin: want builtin, got %q", m.Origin)
		}
		if !contains(m.Members, "sqrt") || !contains(m.Members, "pi") {
			t.Errorf("math members missing sqrt/pi: %v", m.Members)
		}
	}
	// A flat-shape builtin (go_idiomatic) exposes its top-level names.
	if m, ok := findModule(sf, "go_idiomatic"); !ok {
		t.Error("go_idiomatic not in Safe surface")
	} else if !contains(m.Members, "sleep") || !contains(m.Members, "exit") {
		t.Errorf("go_idiomatic members missing sleep/exit: %v", m.Members)
	}
	// The pure newcomers admitted to Safe carry members too.
	if m, ok := findModule(sf, "json"); !ok || !contains(m.Members, "encode") {
		t.Errorf("json surface wrong: %+v ok=%v", m, ok)
	}
	// net must NOT be in the Safe surface (BOX-05).
	if _, ok := findModule(sf, "net"); ok {
		t.Error("net leaked into Safe surface")
	}
}

func TestDescribeSurfaceCustom(t *testing.T) {
	b := starbox.New("surface-custom")
	b.AddModuleData("conf", starlark.StringDict{
		"host": starlark.String("localhost"),
		"port": starlark.MakeInt(8080),
	})
	b.AddModuleFunctions("ops", starbox.FuncMap{
		"ping": func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		},
	})
	// An opaque loader: members cannot be known without invoking it, so nil.
	b.AddModuleLoader("opaque", func() (starlark.StringDict, error) {
		return starlark.StringDict{"secret": starlark.None}, nil
	})

	sf, err := b.DescribeSurface()
	if err != nil {
		t.Fatalf("DescribeSurface: %v", err)
	}

	if m, ok := findModule(sf, "conf"); !ok {
		t.Error("conf module missing")
	} else if m.Origin != starbox.OriginCustom || !reflect.DeepEqual(m.Members, []string{"host", "port"}) {
		t.Errorf("conf surface wrong: %+v", m)
	}
	if m, ok := findModule(sf, "ops"); !ok {
		t.Error("ops module missing")
	} else if !reflect.DeepEqual(m.Members, []string{"ping"}) {
		t.Errorf("ops members: want [ping], got %v", m.Members)
	}
	if m, ok := findModule(sf, "opaque"); !ok {
		t.Error("opaque module missing")
	} else if m.Origin != starbox.OriginCustom || m.Members != nil {
		t.Errorf("opaque surface: want custom/nil members, got %+v", m)
	}
}

func TestDescribeSurfaceGlobals(t *testing.T) {
	b := starbox.New("surface-globals")
	b.AddKeyValue("count", 42)    // raw Go int
	b.AddKeyValue("name", "Star") // raw Go string
	b.AddBuiltin("greet", func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		return starlark.None, nil
	})
	b.AddKeyStarlarkValue("seven", starlark.MakeInt(7))

	sf, err := b.DescribeSurface()
	if err != nil {
		t.Fatalf("DescribeSurface: %v", err)
	}

	want := map[string]string{
		"count": "int",
		"name":  "string",
		"greet": "builtin_function_or_method",
		"seven": "int",
	}
	got := make(map[string]string, len(sf.Globals))
	for _, g := range sf.Globals {
		got[g.Name] = g.Type
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("global %q: want type %q, got %q", name, typ, got[name])
		}
	}
}

func TestDescribeSurfaceScriptDynamic(t *testing.T) {
	b := starbox.New("surface-sd")
	b.AddModuleScript("helper", `value = 1`)
	b.AddNamedModules("mystery") // not builtin, not custom -> routes to dynamic
	b.SetDynamicModuleLoader(func(string) (starlet.ModuleLoader, error) { return nil, nil })

	sf, err := b.DescribeSurface()
	if err != nil {
		t.Fatalf("DescribeSurface: %v", err)
	}
	if m, ok := findModule(sf, "helper.star"); !ok {
		t.Error("script module helper.star missing")
	} else if m.Origin != starbox.OriginScript || m.Members != nil {
		t.Errorf("script surface: want script/nil, got %+v", m)
	}
	if m, ok := findModule(sf, "mystery"); !ok {
		t.Error("dynamic module mystery missing")
	} else if m.Origin != starbox.OriginDynamic || m.Members != nil {
		t.Errorf("dynamic surface: want dynamic/nil, got %+v", m)
	}
}

func TestDescribeSurfaceSideEffectFree(t *testing.T) {
	b := starbox.New("surface-pure")
	b.SetPrintFunc(noopPrint)
	b.SetModuleSet(starbox.SafeModuleSet)
	b.AddKeyValue("word", "World")

	// Describing the surface before any run must not execute the script or
	// lock out further configuration.
	if _, err := b.DescribeSurface(); err != nil {
		t.Fatalf("DescribeSurface before run: %v", err)
	}
	// A normal run still works afterwards (DescribeSurface did not mark hasExec).
	out, err := b.Run(hereDoc(`x = word.upper()`))
	if err != nil {
		t.Fatalf("Run after DescribeSurface: %v", err)
	}
	if out["x"] != "WORLD" {
		t.Errorf("want x=WORLD, got %v", out["x"])
	}
	// And the surface is still describable after the run.
	if _, err := b.DescribeSurface(); err != nil {
		t.Errorf("DescribeSurface after run: %v", err)
	}
}

func TestDescribeSurfaceEdges(t *testing.T) {
	// Unknown module set surfaces the same error Run would return.
	bad := starbox.New("surface-bad")
	bad.SetModuleSet(starbox.ModuleSetName("nope"))
	if _, err := bad.DescribeSurface(); err == nil {
		t.Error("expected error for unknown module set, got nil")
	}

	// A nil host global is reported as the Starlark None type.
	b := starbox.New("surface-nil")
	b.AddKeyValue("zilch", nil)
	sf, err := b.DescribeSurface()
	if err != nil {
		t.Fatalf("DescribeSurface: %v", err)
	}
	var found bool
	for _, g := range sf.Globals {
		if g.Name == "zilch" {
			found = true
			if g.Type != "NoneType" {
				t.Errorf("nil global type: want NoneType, got %q", g.Type)
			}
		}
	}
	if !found {
		t.Error("nil global zilch missing from surface")
	}
}
