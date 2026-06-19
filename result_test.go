package starbox_test

// Result-slot (STAR-10 / BOX-10 / R10) tests:
//   - TestResultBuiltin captures a single structured result via output()
//   - TestResultOnce    a second output() call within a run is an error
//   - TestResultUnset   GetResult reports not-set before a run / when unused
//   - TestResultResetPerRun the slot is once-PER-RUN, reset at each run start

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

// TestResultResetPerRun locks the per-run semantic of output(): the result
// slot is reset at the start of each run, so a reused Box can run repeatedly (a
// second run must not see the slot as "already set once"). A run that does not
// call output() reports unset even after a prior run set it.
func TestResultResetPerRun(t *testing.T) {
	b := starbox.New("result-perrun")
	b.SetPrintFunc(noopPrint)
	b.AddResultBuiltin("output")

	if _, err := b.Run(hereDoc(`output(1)`)); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if v, ok := b.GetResult(); !ok || v.String() != "1" {
		t.Fatalf("run1 result = %v ok=%v, want 1", v, ok)
	}
	// second run reuses the box; the slot must be reset, not "already set once".
	if _, err := b.Run(hereDoc(`output(2)`)); err != nil {
		t.Fatalf("run2 should reset the result slot: %v", err)
	}
	if v, ok := b.GetResult(); !ok || v.String() != "2" {
		t.Errorf("run2 result = %v ok=%v, want 2", v, ok)
	}
	// a run that never calls output() reports unset, even after a prior set.
	if _, err := b.Run(hereDoc(`z = 3`)); err != nil {
		t.Fatalf("run3: %v", err)
	}
	if _, ok := b.GetResult(); ok {
		t.Error("run3 called no output(); GetResult should report unset")
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
