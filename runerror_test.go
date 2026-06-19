package starbox_test

// RunError classification (STAR-8 / BOX-08 / R9) tests:
//   - TestClassifyRunError   nil, arbitrary-error (Unknown, fail-loud), String() names
//   - TestRunErrorKinds       each failure mode classifies to its Kind (chain preserved)
//   - TestRunErrorPassthrough errors.As reaches the original typed error THROUGH a RunError

import (
	"errors"
	"strings"
	"testing"

	"github.com/1set/starbox"
	"github.com/1set/starlet"
)

func TestClassifyRunError(t *testing.T) {
	if starbox.ClassifyRunError(nil) != nil {
		t.Error("ClassifyRunError(nil) should be nil")
	}

	// An unrecognised error is Unknown (fail-loud zero value); the chain is kept.
	base := errors.New("something else")
	re := starbox.ClassifyRunError(base)
	if re.Kind != starbox.RunErrorUnknown {
		t.Errorf("arbitrary error: Kind = %v, want unknown", re.Kind)
	}
	if !errors.Is(re, base) {
		t.Error("RunError should unwrap to the original error")
	}
	if !strings.Contains(re.Error(), "unknown") {
		t.Errorf("RunError.Error() = %q, want mention of kind", re.Error())
	}
	// A zero/empty RunError stringifies safely (the nil-underlying guard).
	if got := (&starbox.RunError{}).Error(); got == "" {
		t.Error("empty RunError.Error() should be non-empty")
	}

	// String() is stable per kind.
	for k, want := range map[starbox.RunErrorKind]string{
		starbox.RunErrorUnknown:        "unknown",
		starbox.RunErrorSyntax:         "syntax",
		starbox.RunErrorCompile:        "compile",
		starbox.RunErrorModuleWithheld: "module_withheld",
		starbox.RunErrorMaxSteps:       "max_steps",
		starbox.RunErrorOutputLimit:    "output_limit",
		starbox.RunErrorEval:           "eval",
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestRunErrorKinds(t *testing.T) {
	cases := []struct {
		name   string
		kind   starbox.RunErrorKind
		build  func(*starbox.Starbox)
		script string
	}{
		{"syntax", starbox.RunErrorSyntax, nil, `x = (`},
		{"compile", starbox.RunErrorCompile, nil, `y = nope`},
		{"eval", starbox.RunErrorEval, nil, `x = 1 // 0`},
		{"module_withheld", starbox.RunErrorModuleWithheld, func(b *starbox.Starbox) { b.SetModuleSet(starbox.SafeModuleSet) }, `load("file", "x")`},
		{"max_steps", starbox.RunErrorMaxSteps, func(b *starbox.Starbox) { b.SetMaxExecutionSteps(1000) }, "s = 0\nfor i in range(100000000):\n\ts += i"},
		{"output_limit", starbox.RunErrorOutputLimit, func(b *starbox.Starbox) { b.SetMaxOutputEntries(1) }, "a = 1\nb = 2\nc = 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := starbox.New("re")
			b.SetPrintFunc(noopPrint)
			if tc.build != nil {
				tc.build(b)
			}
			_, err := b.Run(hereDoc(tc.script))
			re := starbox.ClassifyRunError(err)
			if re == nil {
				t.Fatalf("want error, got nil")
			}
			if re.Kind != tc.kind {
				t.Errorf("Kind = %v, want %v (err=%v)", re.Kind, tc.kind, err)
			}
			// The underlying error is preserved (some Starlark errors are
			// uncomparable, so check non-nil rather than == err).
			if errors.Unwrap(re) == nil {
				t.Error("RunError.Unwrap should expose the underlying error")
			}
		})
	}
}

func TestRunErrorPassthrough(t *testing.T) {
	// errors.As must reach the original typed error THROUGH the RunError wrapper
	// (the R9 "preserve the host's original error" contract).
	b := starbox.New("passthrough")
	b.SetPrintFunc(noopPrint)
	b.SetMaxExecutionSteps(1000)
	_, err := b.Run(hereDoc("s = 0\nfor i in range(100000000):\n\ts += i"))
	re := starbox.ClassifyRunError(err)
	if re == nil || re.Kind != starbox.RunErrorMaxSteps {
		t.Fatalf("want max_steps RunError, got %v", re)
	}
	var mse starlet.MaxStepsExceededError
	if !errors.As(re, &mse) {
		t.Error("errors.As should reach MaxStepsExceededError through the RunError")
	}
}
