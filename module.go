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
	sp, sl, sn, err := extractStarletModules(s.modSet, s.namedMods)
	if err != nil {
		return nil, nil, nil, err
	}

	// extract custom module loaders
	cp, cl, cn := extractLocalModules(s.loadMods, stringsMapSet(sn))

	// extract dynamic module loaders
	existMods := stringsMapSet(sn, cn)
	dp, dl, dn, err := extractDynamicModules(s.dynMods, s.namedMods, existMods)
	if err != nil {
		return nil, nil, nil, err
	}

	// merge all module loaders
	preMods = make(starlet.ModuleLoaderList, 0, len(sp)+len(cp)+len(dp))
	for _, mods := range []starlet.ModuleLoaderList{sp, cp, dp} {
		preMods = append(preMods, mods...)
	}
	lazyMods = make(starlet.ModuleLoaderMap, len(sl)+len(cl)+len(dl))
	for _, mods := range []starlet.ModuleLoaderMap{sl, cl, dl} {
		lazyMods.Merge(mods)
	}
	nameSet := stringsMapSet(sn, cn, dn)
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

// extractLocalModules extracts custom module loaders and ignores loaded starlet builtin modules.
func extractLocalModules(loadMods starlet.ModuleLoaderMap, existMods map[string]struct{}) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string) {
	// separate custom module loaders from starlet module names
	// i.e. ignore conflicts with starlet builtin modules
	for name := range loadMods {
		if _, ok := existMods[name]; !ok {
			modNames = append(modNames, name)
		}
	}

	// convert custom module names to module loaders
	if len(modNames) > 0 {
		preMods = make(starlet.ModuleLoaderList, 0, len(modNames))
		lazyMods = make(starlet.ModuleLoaderMap, len(modNames))
		for _, name := range modNames {
			if loader, ok := loadMods[name]; ok {
				preMods = append(preMods, loader)
				lazyMods[name] = loader
			}
		}
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
		// skip loaded modules
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
