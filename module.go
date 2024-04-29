package starbox

import (
	"fmt"

	"github.com/1set/starlet"
	// lrt "github.com/1set/starbox/module/runtime"
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
	localModuleLoaders = starlet.ModuleLoaderMap{
		// lrt.ModuleName: lrt.LoadModule,
	}
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
