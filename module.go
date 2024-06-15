package starbox

import (
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

	// TODO: undone
	return sp, sl, sn, err
}

// extractStarletModules extracts starlet builtin module loaders from the given module set and additional module names.
func extractStarletModules(setName ModuleSetName, nameMods []string) (preMods starlet.ModuleLoaderList, lazyMods starlet.ModuleLoaderMap, modNames []string, err error) {
	// get starlet modules by set name
	if modNames, err = getModuleSet(setName); err != nil {
		return nil, nil, nil, err
	}

	// append additional starlet module names
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

	// done
	return
}
