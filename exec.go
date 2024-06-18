package starbox

import (
	"errors"
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
	return s.mac.Run()
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
	return s.mac.RunFile(file, s.modFS, nil)
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
	return s.mac.RunWithTimeout(timeout, nil)
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
	return out, err
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
	return out, err
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
		for fp, scr := range s.scriptMods {
			// TODO: support directory/file.star later
			if err := rootFS.WriteFile(fp, []byte(scr), 0644); err != nil {
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
