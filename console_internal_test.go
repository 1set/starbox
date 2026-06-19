package starbox

// White-box coverage for the consoleCore zapcore.Core internals that the
// script-facing log module never drives directly: With (context fields merged
// ahead of call-site fields) and Sync (no-op).

import "testing"

func TestConsoleCoreWithAndSync(t *testing.T) {
	c := &Console{}
	lg := newConsoleLogger(c)

	// With() attaches context fields that must appear on every later entry,
	// merged ahead of the call-site fields.
	lg.With("ctx", "base").Infow("msg", "k", "v")

	got := c.Drain()
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got), got)
	}
	fm := make(map[string]interface{}, len(got[0].Fields))
	for _, f := range got[0].Fields {
		fm[f.Key] = f.Value
	}
	if fm["ctx"] != "base" {
		t.Errorf("context field ctx = %v, want base", fm["ctx"])
	}
	if fm["k"] != "v" {
		t.Errorf("call field k = %v, want v", fm["k"])
	}

	// Sync is a no-op and must not error.
	if err := lg.Sync(); err != nil {
		t.Errorf("Sync: %v", err)
	}
}
