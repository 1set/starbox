// Package starbox provides a comprehensive set of utilities for building and managing Starlark virtual machines with ease.
//
// # Module Loading
//
// Starbox supports various ways to load modules, including preloading built-in modules, adding custom modules, and dynamic module loading at runtime.
//
// Preloading Built-in Modules:
//   - SetModuleSet(modSet ModuleSetName): Preloads a predefined set of modules before execution. Available sets include:
//   - EmptyModuleSet: No modules.
//   - SafeModuleSet: Safe modules without side effects.
//   - NetworkModuleSet: Safe modules plus network modules.
//   - FullModuleSet: All available modules.
//
// Adding Custom Modules:
//   - AddModuleLoader(moduleName string, moduleLoader starlet.ModuleLoader): Adds a custom module loader.
//   - AddModuleFunctions(name string, funcs FuncMap): Adds a module with custom functions.
//   - AddModuleData(moduleName string, moduleData starlark.StringDict): Adds a module with custom data.
//   - AddStructFunctions(name string, funcs FuncMap): Adds a module with custom struct functions.
//   - AddStructData(structName string, structData starlark.StringDict): Adds a module with custom struct data.
//
// Dynamic Module Loading:
//   - SetDynamicModuleLoader(loader DynamicModuleLoader): Sets a dynamic module loader, which loads modules based on their names before execution.
//
// Adding Named Modules:
//   - AddNamedModules(moduleNames ...string): Adds built-in or custom modules by their names.
//   - AddModulesByName(moduleNames ...string): Alias for AddNamedModules.
//
// Modules are loaded in the following order of priority before execution:
//   1. Preloaded Starlet modules from predefined sets and named Starlet modules.
//   2. Custom modules added by users, ignoring modules with the same names as preloaded Starlet modules.
//   3. Dynamically loaded modules based on their names just before execution.
//   4. If a module name is not found in any of the preloaded, custom, or dynamic modules, an error is returned.
package starbox

import (
	"bitbucket.org/neiku/hlog"
	"go.uber.org/zap"
)

var log *zap.SugaredLogger

func init() {
	log = hlog.NewNoopLogger().SugaredLogger
}

// SetLog sets the logger from outside the package.
func SetLog(l *zap.SugaredLogger) {
	log = l
}
