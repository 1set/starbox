package starbox

import (
	"errors"
	"fmt"

	"github.com/1set/starlet"
	slog "github.com/1set/starlet/lib/log"
)

// ModuleSetName defines the name of a module set.
//
// When a script load()s a module that exists in Starlet but is not part of the
// active set, the run fails with a ModuleWithheldError (reachable via
// errors.As) - distinct from load()ing a non-existent module (a "not found"
// error) and from referencing an undefined name (a resolve error).
type ModuleSetName string

const (
	// EmptyModuleSet represents the predefined module set for empty scripts, it contains no modules.
	EmptyModuleSet ModuleSetName = "none"
	// SafeModuleSet represents the predefined module set for safe scripts, it contains only safe modules that do not have side effects with outside world.
	SafeModuleSet ModuleSetName = "safe"
	// NetworkModuleSet represents the predefined module set for network scripts, it's based on SafeModuleSet with additional network modules.
	NetworkModuleSet ModuleSetName = "network"
	// FullModuleSet represents the predefined module set for full scripts, it includes all available modules.
	FullModuleSet ModuleSetName = "full"
)

var (
	fullModuleNames = starlet.GetAllBuiltinModuleNames()

	// safeModuleNames is an explicit allowlist of side-effect-free modules:
	// none of them can reach the network or the filesystem. It is deliberately
	// NOT derived by subtraction from starlet's full module set - that was
	// BOX-05, where upgrading starlet silently slipped its new modules (notably
	// net) into the Safe set. A new starlet module is withheld from Safe until
	// it is classified here by hand; TestModuleSetsGolden fails until it is,
	// and also proves no Safe member carries CapNetwork or CapFileSystem.
	//
	// go_idiomatic is intentionally kept: it supplies the idiomatic globals
	// (sleep/exit and the print helpers). starlet classifies it as
	// CapLog|CapProcess, which the Safe tier tolerates - the hard line Safe
	// must never cross is network or filesystem access.
	safeModuleNames = []string{
		"atom", "base64", "csv", "go_idiomatic", "hashlib", "json", "math",
		"random", "re", "regex", "serial", "stats", "string", "struct", "time",
	}

	// networkExtraNames are layered on top of Safe for NetworkModuleSet:
	// outward I/O (http, net) plus log, which is retained at this tier for
	// backward compatibility (it is a logging side effect, never part of Safe).
	networkExtraNames = []string{"http", "log", "net"}

	moduleSets = map[ModuleSetName][]string{
		EmptyModuleSet:   {},
		SafeModuleSet:    appendUniques(safeModuleNames),
		NetworkModuleSet: appendUniques(safeModuleNames, networkExtraNames...),
		FullModuleSet:    appendUniques(fullModuleNames),
	}
	localModuleLoaders = starlet.ModuleLoaderMap{}
)

// getModuleSet returns the module names for the given module set name.
func getModuleSet(modSet ModuleSetName) ([]string, error) {
	if mods, ok := moduleSets[modSet]; ok {
		return mods, nil
	}
	if modSet == "" {
		return []string{}, nil
	}
	return nil, fmt.Errorf("unknown module set: %s", modSet)
}

func (s *Starbox) extractModLoaders() (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
	// extract starlet builtin module loaders
	starPre, starLazy, starName, err := s.extractStarletModules(s.modSet, s.namedMods)
	if err != nil {
		return nil, nil, nil, err
	}

	// extract custom module loaders
	cusPre, cusLazy, cusName := extractLocalModules(s.loadMods, stringsMapSet(starName))

	// extract dynamic module loaders
	dynPre, dynLazy, dynName, err := extractDynamicModules(s.dynMods, s.namedMods, stringsMapSet(starName, cusName))
	if err != nil {
		return nil, nil, nil, err
	}

	// merge all module loaders
	preMods = make(starlet.ModuleLoaderList, 0, len(starPre)+len(cusPre)+len(dynPre))
	for _, mods := range []starlet.ModuleLoaderList{starPre, cusPre, dynPre} {
		preMods = append(preMods, mods...)
	}
	lazyMods = make(starlet.ModuleLoaderMap, len(starLazy)+len(cusLazy)+len(dynLazy))
	for _, mods := range []starlet.ModuleLoaderMap{starLazy, cusLazy, dynLazy} {
		lazyMods.Merge(mods)
	}
	nameSet := stringsMapSet(starName, cusName, dynName)
	modNames = mapSetStrings(nameSet)

	// all done
	return
}

// extractStarletModules extracts starlet builtin module loaders from the given module set and additional module names.
func (s *Starbox) extractStarletModules(setName ModuleSetName, nameMods []string) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
	// get starlet modules by set name
	if modNames, err = getModuleSet(setName); err != nil {
		return nil, nil, nil, err
	}

	// append additional starlet module by individual names
	addNames := intersectStrings(fullModuleNames, nameMods)
	modNames = appendUniques(modNames, addNames...)

	// convert starlet builtin module names to module loaders
	if len(modNames) > 0 {
		// replace user log module with the custom one
		var (
			leftNames   = make([]string, 0, len(modNames))
			repPreMods  = make(starlet.ModuleLoaderList, 0, 1)
			repLazyMods = make(starlet.ModuleLoaderMap, 1)
		)
		for _, name := range modNames {
			if name == "log" && s.userLog != nil {
				ld := slog.NewModule(s.userLog).LoadModule
				repPreMods = append(repPreMods, ld)
				repLazyMods[name] = ld
			} else {
				leftNames = append(leftNames, name)
			}
		}

		// load vanilla starlet modules first
		if preMods, err = starlet.MakeBuiltinModuleLoaderList(leftNames...); err != nil {
			return nil, nil, nil, err
		}
		if lazyMods, err = starlet.MakeBuiltinModuleLoaderMap(leftNames...); err != nil {
			return nil, nil, nil, err
		}

		// append custom log module if exists
		if len(repPreMods) > 0 {
			preMods = append(preMods, repPreMods...)
			lazyMods.Merge(repLazyMods)
		}
	}
	return
}

// extractLocalModules extracts custom module loaders.
func extractLocalModules(loadMods starlet.ModuleLoaderMap, existMods map[string]struct{}) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string) {
	// no custom module loaders
	if len(loadMods) == 0 {
		return
	}

	// extract all custom module loaders
	preMods = make(starlet.ModuleLoaderList, 0, len(loadMods))
	lazyMods = make(starlet.ModuleLoaderMap, len(loadMods))
	for name, loader := range loadMods {
		// skip loaded modules, i.e. avoid conflicts with starlet builtin modules
		if _, ok := existMods[name]; ok {
			continue
		}
		preMods = append(preMods, loader)
		lazyMods[name] = loader
		modNames = append(modNames, name)
	}
	return
}

var (
	// ErrModuleNotFound is the error for module cannot be found by name.
	ErrModuleNotFound = errors.New("module not found")
)

// ModuleWithheldError reports that a script load()ed a module that exists in
// Starlet but is not part of the Box's active module set. It is re-exported
// from Starlet (the same type Starlet returns) so callers can match it via
// errors.As without importing starlet directly. This is distinct from
// ErrModuleNotFound, which marks a module that does not exist at all.
type ModuleWithheldError = starlet.ModuleWithheldError

// extractDynamicModules extracts dynamic module loaders by module names.
func extractDynamicModules(metaLoad DynamicModuleLoader, nameMods []string, existMods map[string]struct{}) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
	// initialize
	preMods = make(starlet.ModuleLoaderList, 0, len(nameMods))
	lazyMods = make(starlet.ModuleLoaderMap, len(nameMods))

	// get dynamic module loaders by name
	for _, name := range nameMods {
		// skip loaded modules, i.e. dynamic modules acts as a complement to static modules
		if _, ok := existMods[name]; ok {
			continue
		}

		// if no meta loader for unknown module name, return error
		if metaLoad == nil {
			err = ErrModuleNotFound
			return
		}

		// try to load module by name, return error if failed or not found
		var loader starlet.ModuleLoader
		loader, err = metaLoad(name)
		if err != nil {
			return
		}
		if loader == nil {
			err = fmt.Errorf("%w: %s", ErrModuleNotFound, name)
			return
		}

		// for valid loader
		preMods = append(preMods, loader)
		lazyMods[name] = loader
		modNames = append(modNames, name)
	}
	return
}
