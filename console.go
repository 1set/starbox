package starbox

import (
	"sync"
	"time"

	"go.starlark.net/starlark"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LevelPrint is the Level of a ConsoleEntry produced by a print() call (the
// log.* entries carry their zap level name: "debug", "info", "warn", "error").
const LevelPrint = "print"

// ConsoleField is one structured key/value attached to a captured log entry.
// The value is the raw Go value the script passed, never pre-rendered into a
// string - the caller decides how to format it.
type ConsoleField struct {
	Key   string
	Value interface{}
}

// ConsoleEntry is a single piece of console output captured during a run: a
// print() call or a log.<level>() call from the script.
type ConsoleEntry struct {
	// Time is when the entry was captured.
	Time time.Time
	// Level is LevelPrint for print(), or the zap level name for a log.* call.
	Level string
	// Message is the message text. A log.* call's positional arguments are
	// folded into it exactly as the log module renders them; its keyword
	// arguments are kept structured in Fields instead.
	Message string
	// Fields holds a log.* call's keyword arguments verbatim; nil for print().
	Fields []ConsoleField
}

// Console buffers console output captured during runs when a Box has console
// capture enabled (see Starbox.EnableConsoleCapture). It is safe for concurrent
// use: a run appends to it under lock while the caller drains it from another
// goroutine.
type Console struct {
	mu      sync.Mutex
	entries []ConsoleEntry
}

// add appends an entry under the lock.
func (c *Console) add(e ConsoleEntry) {
	c.mu.Lock()
	c.entries = append(c.entries, e)
	c.mu.Unlock()
}

// Drain returns the buffered entries and clears the buffer, so the next run
// starts empty - the per-run drain pattern. It returns nil when nothing was
// captured.
func (c *Console) Drain() []ConsoleEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) == 0 {
		return nil
	}
	out := c.entries
	c.entries = nil
	return out
}

// Len reports how many entries are currently buffered (not yet drained).
func (c *Console) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// consoleCore is a zapcore.Core that records every log entry into a Console,
// keeping the structured fields verbatim instead of rendering them to text.
type consoleCore struct {
	console *Console
	fields  []zapcore.Field
}

// Enabled records every level; level filtering is the script author's concern,
// not the capture layer's.
func (c *consoleCore) Enabled(zapcore.Level) bool { return true }

// With returns a child core carrying the accumulated context fields.
func (c *consoleCore) With(fields []zapcore.Field) zapcore.Core {
	merged := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	merged = append(merged, c.fields...)
	merged = append(merged, fields...)
	return &consoleCore{console: c.console, fields: merged}
}

// Check enqueues this core to write the entry.
func (c *consoleCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.AddCore(ent, c)
}

// Write records the entry and its fields into the Console.
func (c *consoleCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	all := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	all = append(all, c.fields...)
	all = append(all, fields...)

	var cf []ConsoleField
	if len(all) > 0 {
		cf = make([]ConsoleField, 0, len(all))
		for _, f := range all {
			// Recover each field's raw value without a per-type switch: a
			// one-field MapObjectEncoder yields {f.Key: value}.
			enc := zapcore.NewMapObjectEncoder()
			f.AddTo(enc)
			cf = append(cf, ConsoleField{Key: f.Key, Value: enc.Fields[f.Key]})
		}
	}

	c.console.add(ConsoleEntry{
		Time:    ent.Time,
		Level:   ent.Level.String(),
		Message: ent.Message,
		Fields:  cf,
	})
	return nil
}

// Sync is a no-op: the Console buffers in memory, nothing to flush.
func (c *consoleCore) Sync() error { return nil }

// newConsoleLogger builds a SugaredLogger whose output is captured by c.
func newConsoleLogger(c *Console) *zap.SugaredLogger {
	return zap.New(&consoleCore{console: c}).Sugar()
}

// EnableConsoleCapture routes the script's console output into an in-memory,
// drainable Console instead of stderr: print() becomes a LevelPrint entry, and
// the log module's calls (when log is loaded) become leveled entries with their
// keyword arguments preserved as structured Fields. It returns the Console; call
// Console.Drain after each run to collect that run's output.
//
// It replaces both the print function and the log module's logger, so it takes
// precedence over SetPrintFunc and SetLogger - enable capture last, or do not
// mix them. It panics if called after execution.
func (s *Starbox) EnableConsoleCapture() *Console {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot enable console capture after execution")
	}
	c := &Console{}
	s.console = c
	s.printFunc = func(thread *starlark.Thread, msg string) {
		c.add(ConsoleEntry{Time: time.Now(), Level: LevelPrint, Message: msg})
	}
	s.userLog = newConsoleLogger(c)
	return c
}

// Console returns the capture buffer set up by EnableConsoleCapture, or nil if
// console capture was never enabled on this Box.
func (s *Starbox) Console() *Console {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.console
}
