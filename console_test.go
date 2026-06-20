package starbox

// console capture (BOX-06 / STAR-13) tests. This is an internal (white-box)
// test file so the public API and the unexported consoleCore live in one place:
//   - TestConsoleCapturePrint   print() funnels into LevelPrint entries, no fields
//   - TestConsoleCaptureLog     log.* funnels into leveled entries; kwargs kept as raw Fields
//   - TestConsoleCaptureOrder   print and log interleave in script call order
//   - TestConsoleDrainPerRun    Drain returns then clears; successive runs accumulate independently
//   - TestConsoleNotEnabled     Console() is nil until EnableConsoleCapture
//   - TestConsoleCoreWithAndSync consoleCore With/Sync internals the log module never drives

import (
	"fmt"
	"testing"
)

// fieldMap flattens captured fields to a lookup keyed by field name.
func fieldMap(fs []ConsoleField) map[string]interface{} {
	m := make(map[string]interface{}, len(fs))
	for _, f := range fs {
		m[f.Key] = f.Value
	}
	return m
}

func TestConsoleCapturePrint(t *testing.T) {
	b := New("cap")
	con := b.EnableConsoleCapture()
	if _, err := b.Run(HereDoc(`
		print("hello")
		print("world")
	`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := con.Drain()
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	for i, want := range []string{"hello", "world"} {
		if got[i].Level != LevelPrint {
			t.Errorf("entry %d: level = %q, want %q", i, got[i].Level, LevelPrint)
		}
		if got[i].Message != want {
			t.Errorf("entry %d: message = %q, want %q", i, got[i].Message, want)
		}
		if got[i].Fields != nil {
			t.Errorf("entry %d: print should carry no fields, got %+v", i, got[i].Fields)
		}
		if got[i].Time.IsZero() {
			t.Errorf("entry %d: time should be set", i)
		}
	}
	if con.Len() != 0 {
		t.Errorf("Drain should clear the buffer, Len = %d", con.Len())
	}
}

func TestConsoleCaptureLog(t *testing.T) {
	b := New("cap")
	b.AddNamedModules("log")
	con := b.EnableConsoleCapture()
	if _, err := b.Run(HereDoc(`
		log.info("hi", user="alice", count=3)
		log.error("boom")
	`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := con.Drain()
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}

	// leveled entry with keyword args preserved as raw fields (not rendered).
	if got[0].Level != "info" {
		t.Errorf("level = %q, want info", got[0].Level)
	}
	if got[0].Message != "hi" {
		t.Errorf("message = %q, want hi", got[0].Message)
	}
	fm := fieldMap(got[0].Fields)
	if fm["user"] != "alice" {
		t.Errorf("field user = %v, want alice", fm["user"])
	}
	if got := fmt.Sprint(fm["count"]); got != "3" {
		t.Errorf("field count = %v (%T), want 3", fm["count"], fm["count"])
	}

	// a log call with no keyword args carries no fields.
	if got[1].Level != "error" || got[1].Message != "boom" {
		t.Errorf("entry 1 = {%q,%q}, want {error,boom}", got[1].Level, got[1].Message)
	}
	if got[1].Fields != nil {
		t.Errorf("entry 1 should carry no fields, got %+v", got[1].Fields)
	}
}

func TestConsoleCaptureOrder(t *testing.T) {
	b := New("cap")
	b.AddNamedModules("log")
	con := b.EnableConsoleCapture()
	if _, err := b.Run(HereDoc(`
		print("a")
		log.warn("b")
		print("c")
	`)); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := con.Drain()
	want := []struct{ level, msg string }{
		{LevelPrint, "a"},
		{"warn", "b"},
		{LevelPrint, "c"},
	}
	if len(got) != len(want) {
		t.Fatalf("want %d entries, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].Level != w.level || got[i].Message != w.msg {
			t.Errorf("entry %d = {%q,%q}, want {%q,%q}", i, got[i].Level, got[i].Message, w.level, w.msg)
		}
	}
}

func TestConsoleDrainPerRun(t *testing.T) {
	b := New("cap")
	con := b.EnableConsoleCapture()

	if _, err := b.Run(`print("run1")`); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if r1 := con.Drain(); len(r1) != 1 || r1[0].Message != "run1" {
		t.Fatalf("run1 drain = %+v", r1)
	}

	// the same Box reruns; the buffer started empty after the drain above.
	if _, err := b.Run(`print("run2")`); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if r2 := con.Drain(); len(r2) != 1 || r2[0].Message != "run2" {
		t.Fatalf("run2 drain = %+v", r2)
	}

	// draining an empty console yields nil.
	if got := con.Drain(); got != nil {
		t.Errorf("empty drain = %+v, want nil", got)
	}
}

func TestConsoleNotEnabled(t *testing.T) {
	b := New("cap")
	if b.Console() != nil {
		t.Errorf("Console() should be nil before EnableConsoleCapture")
	}
}

// TestConsoleCoreWithAndSync covers the consoleCore zapcore.Core internals the
// script-facing log module never drives directly: With (context fields merged
// ahead of call-site fields) and Sync (no-op).
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
	fm := fieldMap(got[0].Fields)
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
