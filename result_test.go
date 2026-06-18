package starbox_test

// Result-slot (STAR-10 / BOX-10 / R10) tests:
//   - TestResultBuiltin captures a single structured result via output()
//   - TestResultOnce    a second output() call within a run is an error
//   - TestResultUnset   GetResult reports not-set before a run / when unused

import (
	"fmt"
	"testing"

	"github.com/1set/starbox"
)

func TestResultBuiltin(t *testing.T) {
	b := starbox.New("result")
	b.SetPrintFunc(noopPrint)
	b.AddResultBuiltin("output")

	if _, ok := b.GetResult(); ok {
		t.Error("result reported set before any run")
	}
	if _, err := b.Run(hereDoc(`output(6 * 7)`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	r, ok := b.GetResult()
	if !ok {
		t.Fatal("result not set after output()")
	}
	if got := fmt.Sprint(r); got != "42" {
		t.Errorf("result: want 42, got %s", got)
	}
}

func TestResultOnce(t *testing.T) {
	b := starbox.New("result-once")
	b.SetPrintFunc(noopPrint)
	b.AddResultBuiltin("output")

	_, err := b.Run(hereDoc("output(1)\noutput(2)"))
	if err == nil {
		t.Error("expected error on second output() call, got nil")
	}
}

func TestResultUnset(t *testing.T) {
	b := starbox.New("result-unset")
	b.SetPrintFunc(noopPrint)
	b.AddResultBuiltin("output")

	// A run that never calls output() leaves the slot unset.
	if _, err := b.Run(hereDoc(`x = 1`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := b.GetResult(); ok {
		t.Error("result reported set though output() was never called")
	}
}

func TestResultArity(t *testing.T) {
	b := starbox.New("result-arity")
	b.SetPrintFunc(noopPrint)
	b.AddResultBuiltin("output")

	// output() requires exactly one argument.
	if _, err := b.Run(hereDoc(`output()`)); err == nil {
		t.Error("expected error for output() with no argument")
	}
}
