package starbox

import (
	"errors"
	"fmt"

	"github.com/1set/starlet"
)

// ModuleSetName defines the name of a module set.
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
	moduleSets      = map[ModuleSetName][]string{
		EmptyModuleSet:   {},
		SafeModuleSet:    removeUniques(fullModuleNames, "file", "path", "runtime", "http", "log"),
		NetworkModuleSet: removeUniques(fullModuleNames, "file", "path", "runtime"),
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
	starPre, starLazy, starName, err := extractStarletModules(s.modSet, s.namedMods)
	if err != nil {
		return nil, nil, nil, err
	}

	// extract custom module loaders
	cusPre, cusLazy, cusName := extractLocalModules(s.loadMods)

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
func extractStarletModules(setName ModuleSetName, nameMods []string) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
	// get starlet modules by set name
	if modNames, err = getModuleSet(setName); err != nil {
		return nil, nil, nil, err
	}

	// append additional starlet module by individual names
	addNames := intersectStrings(fullModuleNames, nameMods)
	modNames = appendUniques(modNames, addNames...)

	// convert starlet builtin module names to module loaders
	if len(modNames) > 0 {
		if preMods, err = starlet.MakeBuiltinModuleLoaderList(modNames...); err != nil {
			return nil, nil, nil, err
		}
		if lazyMods, err = starlet.MakeBuiltinModuleLoaderMap(modNames...); err != nil {
			return nil, nil, nil, err
		}
	}
	return
}

// extractLocalModules extracts custom module loaders.
func extractLocalModules(loadMods starlet.ModuleLoaderMap) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string) {
	// no custom module loaders
	if len(loadMods) == 0 {
		return
	}

	// extract all custom module loaders
	preMods = make(starlet.ModuleLoaderList, 0, len(loadMods))
	lazyMods = make(starlet.ModuleLoaderMap, len(loadMods))
	for name, loader := range loadMods {
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

// extractDynamicModules extracts dynamic module loaders by module names.
func extractDynamicModules(metaLoad DynamicModuleLoader, nameMods []string, existMods map[string]struct{}) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
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
