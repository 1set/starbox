package starbox

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"time"

	"github.com/1set/starlet"
	"github.com/psanford/memfs"
)

// Run executes a script and returns the converted output.
func (s *Starbox) Run(script string) (starlet.StringAnyMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// prepare environment
	if err := s.prepareScriptEnv(script); err != nil {
		return nil, err
	}

	// run
	s.hasExec = true
	s.execTimes++
	return s.applyOutputLimit(s.mac.Run())
}

// RunFile executes a script file and returns the converted output.
func (s *Starbox) RunFile(file string) (starlet.StringAnyMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// prepare environment
	if err := s.prepareEnv(); err != nil {
		return nil, err
	}

	// run
	s.hasExec = true
	s.execTimes++
	return s.applyOutputLimit(s.mac.RunFile(file, s.modFS, nil))
}

// RunTimeout executes a script and returns the converted output.
func (s *Starbox) RunTimeout(script string, timeout time.Duration) (starlet.StringAnyMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// prepare environment
	if err := s.prepareScriptEnv(script); err != nil {
		return nil, err
	}

	// run
	s.hasExec = true
	s.execTimes++
	return s.applyOutputLimit(s.mac.RunWithTimeout(timeout, nil))
}

// REPL starts a REPL session.
func (s *Starbox) REPL() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// prepare environment -- no need to set script content
	if err := s.prepareScriptEnv(""); err != nil {
		return err
	}

	// run
	s.hasExec = true
	s.execTimes++
	s.mac.REPL()
	return nil
}

// RunInspect executes a script and then REPL with result and returns the converted output.
func (s *Starbox) RunInspect(script string) (starlet.StringAnyMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// prepare environment
	if err := s.prepareScriptEnv(script); err != nil {
		return nil, err
	}

	// run script
	s.hasExec = true
	s.execTimes++
	out, err := s.mac.Run()

	// repl
	s.mac.REPL()
	return s.applyOutputLimit(out, err)
}

// InspectCondFunc is a function type for inspecting the converted output of Run*() and decide whether to continue.
type InspectCondFunc func(starlet.StringAnyMap, error) bool

// RunInspectIf executes a script and then REPL with result and returns the converted output, if the condition is met.
// The condition function is called with the converted output and the error from Run*(), and returns true if REPL is needed.
func (s *Starbox) RunInspectIf(script string, cond InspectCondFunc) (starlet.StringAnyMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// prepare environment
	if err := s.prepareScriptEnv(script); err != nil {
		return nil, err
	}

	// run script
	s.hasExec = true
	s.execTimes++
	out, err := s.mac.Run()

	// repl
	if cond(out, err) {
		s.mac.REPL()
	}
	return s.applyOutputLimit(out, err)
}

// CallStarlarkFunc executes a function defined in Starlark with arguments and returns the converted output.
func (s *Starbox) CallStarlarkFunc(name string, args ...interface{}) (interface{}, error) {
	if s == nil || s.mac == nil {
		return nil, errors.New("no starlet machine")
	}

	// lock it
	s.mu.Lock()
	defer s.mu.Unlock()

	// call it
	return s.mac.Call(name, args...)
}

// OutputLimitExceededError marks a run whose result exceeded the configured
// output-entry limit (SetMaxOutputEntries). It is reachable via errors.As and
// is one of the typed run errors STAR-8's RunError taxonomy will classify.
type OutputLimitExceededError struct {
	Limit uint // the configured maximum number of result entries
	Count uint // the number of result entries the run actually produced
}

// Error returns the error message.
func (e OutputLimitExceededError) Error() string {
	return fmt.Sprintf("output exceeded the entry limit (%d > %d)", e.Count, e.Limit)
}

// applyOutputLimit enforces the configured max output-entry count on a
// successful run's result: a result with more than the limit entries is
// withheld and a typed OutputLimitExceededError is returned instead. A zero
// limit (the default) means unlimited. It is a post-hoc policy gate on the
// result, not a memory guard - SetMaxExecutionSteps bounds resource use.
func (s *Starbox) applyOutputLimit(out starlet.StringAnyMap, err error) (starlet.StringAnyMap, error) {
	if err != nil || s.maxOutputEntries == 0 {
		return out, err
	}
	if n := uint(len(out)); n > s.maxOutputEntries {
		return nil, OutputLimitExceededError{Limit: s.maxOutputEntries, Count: n}
	}
	return out, err
}

func (s *Starbox) prepareScriptEnv(script string) (err error) {
	// if it's not the first run, set the script content only
	if s.hasExec {
		s.mac.SetScriptContent([]byte(script))
		return nil
	}

	// prepare environment
	if err = s.prepareEnv(); err != nil {
		return err
	}

	// set script
	s.mac.SetScript("box.star", []byte(script), s.modFS)

	// all is done
	return nil
}

func (s *Starbox) prepareEnv() (err error) {
	// set custom tag and print function
	if s.structTag != "" {
		s.mac.SetCustomTag(s.structTag)
	}
	if s.printFunc != nil {
		s.mac.SetPrintFunc(s.printFunc)
	}

	// set variables
	s.mac.SetGlobals(s.globals)

	// extract module loaders
	preMods, lazyMods, modNames, err := s.extractModLoaders()
	if err != nil {
		return err
	}

	// set modules to machine
	if len(preMods) > 0 || len(lazyMods) > 0 {
		s.mac.SetPreloadModules(preMods)
		s.mac.SetLazyloadModules(lazyMods)
	}

	// prepare script modules
	if len(s.scriptMods) > 0 && s.modFS == nil {
		rootFS := memfs.New()
		// materialize in sorted name order so parent-directory creation and the
		// resulting __modules__ ordering are deterministic, not map-iteration random.
		fps := make([]string, 0, len(s.scriptMods))
		for fp := range s.scriptMods {
			fps = append(fps, fp)
		}
		sort.Strings(fps)
		for _, fp := range fps {
			// policy load gate (A4): a script module the policy does not permit
			// is not materialized, so the script cannot load() it. Gated by the
			// registered (.star) name, consistent with DescribeSurface.
			if !s.policyAllows(fp) {
				continue
			}
			// create parent directories first so a nested-path module script
			// (e.g. "lib/util.star") writes into an existing tree - memfs
			// WriteFile does not create intermediate directories on its own.
			if dir := path.Dir(fp); dir != "." && dir != "/" {
				if err := rootFS.MkdirAll(dir, 0755); err != nil {
					return err
				}
			}
			if err := rootFS.WriteFile(fp, []byte(s.scriptMods[fp]), 0644); err != nil {
				return err
			}
			modNames = append(modNames, fp)
		}
		s.modFS = rootFS
	}

	// set load module names
	s.modNames = modNames
	s.mac.AddGlobals(starlet.StringAnyMap{
		"__modules__": starlarkStringList(modNames),
	})
	return nil
}
