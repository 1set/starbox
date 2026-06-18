package starbox

import (
	"fmt"

	"github.com/1set/starlet"
	"go.starlark.net/starlark"
)

// AddResultBuiltin registers a builtin with the given name (e.g. "output") that
// a script calls once to set its single structured result. A second call within
// a run is an error, so a script cannot ambiguously "return" twice. Retrieve the
// captured value after a run with GetResult.
//
// It panics (DPanic) if called after execution.
func (s *Starbox) AddResultBuiltin(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add result builtin after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.result = nil
	s.resultSet = false
	s.globals[name] = starlark.NewBuiltin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var v starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &v); err != nil {
			return nil, err
		}
		// This runs inside s.mac.Run(), which holds s.mu for the whole
		// execution, so the slot is written under that lock - no extra lock here.
		if s.resultSet {
			return nil, fmt.Errorf("%s: result already set once", b.Name())
		}
		s.result = v
		s.resultSet = true
		return starlark.None, nil
	})
}

// GetResult returns the value captured by the result builtin and whether it was
// set during a run. The value is the raw Starlark value the script passed.
func (s *Starbox) GetResult() (starlark.Value, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.result, s.resultSet
}
