// Package starbox provides a comprehensive set of utilities for building and managing Starlark virtual machines with ease.
//
// # Module Sources
//
// Starbox supports loading modules from various sources, including built-in modules from Starlet, custom modules added by the user, and dynamic modules resolved by name on demand.
//
// Built-in Modules:
//
// Use SetModuleSet(modSet ModuleSetName) to select a predefined set of modules from Starlet to preload before execution.
// Available sets include:
//   - EmptyModuleSet: No modules.
//   - SafeModuleSet: Safe modules without access to the file system or network.
//   - NetworkModuleSet: Safe modules plus network modules.
//   - FullModuleSet: All available modules.
//
// Custom Modules:
//
//   - AddModuleLoader(moduleName string, moduleLoader starlet.ModuleLoader): Adds a custom module loader. Members can be accessed in the script via load("module_name", "member_name") or member_name.
//   - AddModuleFunctions(name string, funcs FuncMap): Adds a module of custom functions. Functions can be accessed in the script via load("module_name", "func_name") or module_name.func_name.
//   - AddModuleData(moduleName string, moduleData starlark.StringDict): Adds a module of custom data. Data can be accessed in the script via load("module_name", "key") or module_name.key.
//   - AddStructFunctions(name string, funcs FuncMap): Adds a struct of custom functions. Functions can be accessed in the script via load("struct_name", "func_name") or struct_name.func_name.
//   - AddStructData(structName string, structData starlark.StringDict): Adds a struct of custom data. Data can be accessed in the script via load("struct_name", "key") or struct_name.key.
//
// Dynamic Modules:
//
//   - SetDynamicModuleLoader(loader DynamicModuleLoader): Sets a dynamic module loader function, which returns module loaders based on their names before execution. These module names should be defined using AddNamedModules or AddModulesByName.
//
// # Module Loading Priority
//
// Modules are loaded in the following order of priority before execution:
//   1. Preloaded Starlet modules from predefined sets and additional Starlet modules by name.
//   2. Custom modules added by users, preloaded Starlet modules with the same names would not be overwritten.
//   3. Dynamically loaded modules based on their names just before execution.
//   4. If a module name is not found in any of the built-in, custom, or dynamic modules, an error is returned.
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
