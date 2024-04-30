package starbox

import (
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

// Reset creates an new Starlet machine and keeps the settings.
func (s *Starbox) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	//s.mac.Reset()
	s.mac = newStarMachine(s.name)
	s.hasExec = false
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
	preMods, lazyMods, modNames, err := s.extractModLoads()
	if err != nil {
		return err
	}

	// set modules to machine
	if len(preMods) > 0 || len(lazyMods) > 0 {
		s.mac.SetPreloadModules(preMods)
		s.mac.SetLazyloadModules(lazyMods)
	}

	// set module names
	s.modNames = modNames
	s.mac.AddGlobals(starlet.StringAnyMap{
		"__modules__": starlarkStringList(modNames),
	})

	// prepare script modules
	if len(s.scriptMods) > 0 && s.modFS == nil {
		rootFS := memfs.New()
		for fp, scr := range s.scriptMods {
			// TODO: support directory/file.star later
			if err := rootFS.WriteFile(fp, []byte(scr), 0644); err != nil {
				return err
			}
		}
		s.modFS = rootFS
	}
	return nil
}

func (s *Starbox) extractModLoads() (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
	// get modules by name: local module set + individual names for starlet
	if modNames, err = getModuleSet(s.modSet); err != nil {
		return nil, nil, nil, err
	}
	modNames = append(modNames, s.builtMods...)
	modNames = uniqueStrings(modNames)

	// separate local module loaders from starlet module names
	var (
		letModNames []string
		modLoads    = make(starlet.ModuleLoaderMap, len(modNames))
	)
	for _, name := range modNames {
		if load, ok := localModuleLoaders[name]; ok {
			// for local module loaders
			modLoads[name] = load
		} else {
			// for starlet module names
			letModNames = append(letModNames, name)
		}
	}
	modNames = letModNames
	modLoads.Merge(s.loadMods) // custom module loaders overwrites local module loaders with the same name

	// convert starlet builtin module names to module loaders
	if len(modNames) > 0 {
		if preMods, err = starlet.MakeBuiltinModuleLoaderList(modNames...); err != nil {
			return nil, nil, nil, err
		}
		if lazyMods, err = starlet.MakeBuiltinModuleLoaderMap(modNames...); err != nil {
			return nil, nil, nil, err
		}
	}

	// merge custom module loaders
	if len(modLoads) > 0 {
		if preMods == nil {
			preMods = make(starlet.ModuleLoaderList, 0, len(modLoads))
		}
		if lazyMods == nil {
			lazyMods = make(starlet.ModuleLoaderMap, len(modLoads))
		}
		for name, loader := range modLoads {
			preMods = append(preMods, loader)
			lazyMods[name] = loader
			modNames = append(modNames, name)
		}
		// remove duplicates
		modNames = uniqueStrings(modNames)
	}

	// result
	return preMods, lazyMods, modNames, nil
}
