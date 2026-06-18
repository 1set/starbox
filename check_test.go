package starbox_test

// Check (STAR-10 / BOX-10 / R8) tests, grouped by concern:
//   - TestCheckValidAndSyntax   clean script vs a syntax error
//   - TestCheckUndefined        undefined names become resolve diagnostics
//   - TestCheckConfiguredNames  Safe-set + custom + globals names resolve; excluded ones do not
//   - TestCheckEdges            unknown module set errors; Check does not execute the script

import (
	"strings"
	"testing"

	"github.com/1set/starbox"
	"go.starlark.net/starlark"
)

func TestCheckValidAndSyntax(t *testing.T) {
	b := starbox.New("check-ok")
	if d, err := b.Check(`x = 1 + 2`); err != nil || d != nil {
		t.Errorf("valid script: diags=%v err=%v", d, err)
	}
	d, err := b.Check(`x = (`)
	if err != nil {
		t.Fatalf("Check syntax: %v", err)
	}
	if len(d) == 0 {
		t.Error("syntax error produced no diagnostic")
	}
}

func TestCheckUndefined(t *testing.T) {
	b := starbox.New("check-undef")
	d, err := b.Check(`y = nope + 1`)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(d) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %v", len(d), d)
	}
	if !strings.Contains(d[0].Msg, "undefined: nope") {
		t.Errorf("diagnostic msg = %q, want mention of undefined: nope", d[0].Msg)
	}
	if d[0].Line != 1 {
		t.Errorf("diagnostic line = %d, want 1", d[0].Line)
	}
	if s := d[0].String(); !strings.HasPrefix(s, "1:") || !strings.Contains(s, "undefined: nope") {
		t.Errorf("Diagnostic.String() = %q", s)
	}
}

func TestCheckConfiguredNames(t *testing.T) {
	b := starbox.New("check-names")
	b.SetModuleSet(starbox.SafeModuleSet)
	b.AddKeyValue("word", "hi")
	b.AddModuleData("conf", starlark.StringDict{"host": starlark.String("localhost")})

	// math (namespaced), sleep (flat from go_idiomatic), a global, and a custom
	// module name all resolve without running.
	if d, err := b.Check("a = math.pi\nsleep(0)\nb = word\nc = conf.host"); err != nil || d != nil {
		t.Errorf("configured names should resolve: diags=%v err=%v", d, err)
	}
	// net is excluded from the Safe set (BOX-05), so it is undefined here.
	d, err := b.Check(`x = net`)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(d) != 1 || !strings.Contains(d[0].Msg, "undefined: net") {
		t.Errorf("want 'undefined: net' diagnostic, got %v", d)
	}
}

func TestCheckEdges(t *testing.T) {
	// Unknown module set surfaces an error (same as Run).
	bad := starbox.New("check-bad")
	bad.SetModuleSet(starbox.ModuleSetName("nope"))
	if _, err := bad.Check(`x = 1`); err == nil {
		t.Error("expected error for unknown module set")
	}

	// Check must not execute the script: a side effect would show here, and a
	// real Run must still work afterwards.
	b := starbox.New("check-pure")
	b.SetPrintFunc(noopPrint)
	b.AddKeyValue("word", "World")
	if _, err := b.Check(`x = word.upper()`); err != nil {
		t.Fatalf("Check: %v", err)
	}
	out, err := b.Run(hereDoc(`x = word.upper()`))
	if err != nil || out["x"] != "WORLD" {
		t.Errorf("Run after Check: out=%v err=%v", out, err)
	}
}
