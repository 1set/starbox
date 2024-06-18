package starbox

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv"
	libhttp "github.com/1set/starlet/lib/http"
	"go.starlark.net/starlark"
)

// StarlarkFunc is a function that can be called from Starlark.
type StarlarkFunc func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)

// FuncMap is a map of Starlark functions.
type FuncMap map[string]StarlarkFunc

// DynamicModuleLoader is a function type that takes a module name as input and returns a corresponding module loader.
// It is invoked before execution to dynamically load modules as needed, and serves as a complement to Starlet's built-in modules and custom-added modules.
// For given module names, if the module is not a built-in module or a custom-added module, this function is called to look it up.
// If the module is not found or fails to initialize, an error is returned.
// For non-existent modules, it should return (nil, nil) or (nil, error).
type DynamicModuleLoader func(string) (starlet.ModuleLoader, error)

// Starbox is a wrapper of starlet.Machine with additional features.
type Starbox struct {
	mac        *starlet.Machine
	mu         sync.RWMutex
	hasExec    bool
	execTimes  uint
	name       string
	structTag  string
	printFunc  starlet.PrintFunc
	globals    starlet.StringAnyMap
	modSet     ModuleSetName
	namedMods  []string
	loadMods   starlet.ModuleLoaderMap
	scriptMods map[string]string
	modFS      fs.FS
	modNames   []string
	dynMods    DynamicModuleLoader
}

// New creates a new Starbox instance with default settings.
func New(name string) *Starbox {
	return &Starbox{mac: newStarMachine(name), name: name}
}

func newStarMachine(name string) *starlet.Machine {
	m := starlet.NewDefault()
	m.EnableGlobalReassign()
	m.SetScriptCacheEnabled(true)
	// m.SetInputConversionEnabled(false)
	// m.SetOutputConversionEnabled(true)
	m.SetPrintFunc(func(thread *starlark.Thread, msg string) {
		prefix := fmt.Sprintf("[⭐|%s](%s)", name, time.Now().UTC().Format(`15:04:05.000`))
		eprintln(prefix, msg)
	})
	return m
}

// String returns the name of the Starbox instance.
func (s *Starbox) String() string {
	return fmt.Sprintf("🥡Box{name:%s,run:%d}", s.name, s.execTimes)
}

// Reset creates an new Starlet machine and keeps the settings.
func (s *Starbox) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	//s.mac.Reset()
	s.mac = newStarMachine(s.name)
	s.hasExec = false
}

// GetMachine returns the underlying starlet.Machine instance.
func (s *Starbox) GetMachine() *starlet.Machine {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.mac
}

// GetSteps returns the computation steps executed by the underlying Starlark thread.
func (s *Starbox) GetSteps() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if m := s.mac; m != nil {
		if t := m.GetStarlarkThread(); t != nil {
			return t.Steps
		}
	}
	return 0
}

// GetModuleNames returns the names of the modules loaded after execution.
func (s *Starbox) GetModuleNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.modNames
}

// SetStructTag sets the custom tag of Go struct fields for Starlark.
// It panics if called after execution.
func (s *Starbox) SetStructTag(tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot set tag after execution")
	}
	s.structTag = tag
}

// SetPrintFunc sets the print function for Starlark.
// It panics if called after execution.
func (s *Starbox) SetPrintFunc(printFunc starlet.PrintFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot set print function after execution")
	}
	s.printFunc = printFunc
}

// SetFS sets the virtual filesystem for module scripts.
// If it's not nil, it'll override all the scripts added by AddModuleScript().
// It panics if called after execution.
func (s *Starbox) SetFS(hfs fs.FS) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot set filesystem after execution")
	}
	s.modFS = hfs
}

// SetScriptCache sets custom cache provider for script content.
// nil cache provider will disable script cache.
// It panics if called after execution.
func (s *Starbox) SetScriptCache(cache starlet.ByteCache) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot set script cache after execution")
	}
	if cache == nil {
		s.mac.SetScriptCacheEnabled(false)
	} else {
		s.mac.SetScriptCache(cache)
	}
}

// SetDynamicModuleLoader sets the dynamic module loader for preload and lazyload modules.
// It panics if called after execution.
func (s *Starbox) SetDynamicModuleLoader(loader DynamicModuleLoader) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot set dynamic module loader after execution")
	}
	s.dynMods = loader
}

// SetModuleSet sets the module set to be loaded before execution.
// It panics if called after execution.
func (s *Starbox) SetModuleSet(modSet ModuleSetName) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot set module set after execution")
	}
	s.modSet = modSet
}

// AddKeyValue adds a key-value pair to the global environment before execution.
// If the key already exists, it will be overwritten.
// It panics if called after execution.
func (s *Starbox) AddKeyValue(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add key-value pair after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals[key] = value
}

// AddKeyStarlarkValue adds a key-value pair to the global environment before execution, the value is a Starlark value.
// If the key already exists, it will be overwritten.
// It panics if called after execution.
func (s *Starbox) AddKeyStarlarkValue(key string, value starlark.Value) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add key-value pair after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals[key] = value
}

// AddKeyValues adds key-value pairs to the global environment before execution. Usually for output of Run()*.
// For each key-value pair, if the key already exists, it will be overwritten.
// It panics if called after execution.
func (s *Starbox) AddKeyValues(keyValues starlet.StringAnyMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add key-value pairs after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals.Merge(keyValues)
}

// AddStarlarkValues adds key-value pairs to the global environment before execution, the values are already converted to Starlark values.
// For each key-value pair, if the key already exists, it will be overwritten.
// It panics if called after execution.
func (s *Starbox) AddStarlarkValues(keyValues starlark.StringDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add key-value pairs after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	for key, value := range keyValues {
		s.globals[key] = value
	}
}

// AddBuiltin adds a builtin function with name to the global environment before execution.
// If the name already exists, it will be overwritten.
// It panics if called after execution.
func (s *Starbox) AddBuiltin(name string, starFunc StarlarkFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add builtin after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	sb := starlark.NewBuiltin(name, starFunc)
	s.globals[name] = sb
}

// AddNamedModules adds builtin and custom modules by name to the preload and lazyload registry.
// It will not load the modules until the first run.
// It panics if called after execution.
func (s *Starbox) AddNamedModules(moduleNames ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add named modules after execution")
	}
	s.namedMods = append(s.namedMods, moduleNames...)
}

// AddModulesByName is an alias of AddNamedModules().
func (s *Starbox) AddModulesByName(moduleNames ...string) {
	s.AddNamedModules(moduleNames...)
}

// AddModuleLoader adds a custom module loader to the preload and lazyload registry.
// It will not load the module until the first run, and load result can be accessed in script via load("module_name", "key1") or key1 directly.
// It panics if called after execution.
func (s *Starbox) AddModuleLoader(moduleName string, moduleLoader starlet.ModuleLoader) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add module loader after execution")
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	s.loadMods[moduleName] = moduleLoader
}

// AddModuleFunctions adds a module with the given module functions along with a module loader, and adds it to the preload and lazyload registry.
// The given module function can be accessed in script via load("module_name", "func1") or module_name.func1.
// It works like AddModuleData() but allows only functions as values.
// It panics if called after execution.
func (s *Starbox) AddModuleFunctions(name string, funcs FuncMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add module function after execution")
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	sfd := starlark.StringDict{}
	for fn, fv := range funcs {
		sfd[fn] = starlark.NewBuiltin(name+"."+fn, fv)
	}
	s.loadMods[name] = dataconv.WrapModuleData(name, sfd)
}

// AddModuleData creates a module for the given module data along with a module loader, and adds it to the preload and lazyload registry.
// The given module data can be accessed in script via load("module_name", "key1") or module_name.key1.
// It panics if called after execution.
func (s *Starbox) AddModuleData(moduleName string, moduleData starlark.StringDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add module data after execution")
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	s.loadMods[moduleName] = dataconv.WrapModuleData(moduleName, moduleData)
}

// AddStructFunctions adds a module with the given struct functions along with a module loader, and adds it to the preload and lazyload registry.
// The given struct function can be accessed in script via load("struct_name", "func1") or struct_name.func1.
// It works like AddStructData() but allows only functions as values.
// It panics if called after execution.
func (s *Starbox) AddStructFunctions(name string, funcs FuncMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add struct function after execution")
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	sfd := starlark.StringDict{}
	for fn, fv := range funcs {
		sfd[fn] = starlark.NewBuiltin(name+"."+fn, fv)
	}
	s.loadMods[name] = dataconv.WrapStructData(name, sfd)
}

// AddStructData creates a module for the given struct data along with a module loader, and adds it to the preload and lazyload registry.
// The given struct data can be accessed in script via load("struct_name", "key1") or struct_name.key1.
// It panics if called after execution.
func (s *Starbox) AddStructData(structName string, structData starlark.StringDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add struct data after execution")
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	s.loadMods[structName] = dataconv.WrapStructData(structName, structData)
}

// AddModuleScript creates a module with given module script in virtual filesystem, and adds it to the preload and lazyload registry.
// The given module script can be accessed in script via load("module_name", "key1") or load("module_name.star", "key1") if module name has no ".star" suffix.
// All the module scripts added by this method would be overridden by SetFS() if it's not nil.
// It panics if called after execution.
func (s *Starbox) AddModuleScript(moduleName, moduleScript string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add module script after execution")
	}
	if s.scriptMods == nil {
		s.scriptMods = make(map[string]string)
	}
	name := strings.TrimSpace(moduleName)
	if !strings.HasSuffix(name, ".star") {
		name += ".star"
	}
	s.scriptMods[name] = moduleScript
}

// AddHTTPContext adds HTTP request and response data wrapper to the global environment before execution.
// It takes an HTTP request and returns the response data wrapper for setting response headers and body.
// It panics if called after execution.
func (s *Starbox) AddHTTPContext(req *http.Request) *libhttp.ServerResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add HTTP context after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}

	// add request to globals
	if sr := libhttp.ConvertServerRequest(req); sr != nil {
		s.globals["request"] = sr
	} else {
		s.globals["request"] = starlark.None
	}

	// add response to globals
	resp := libhttp.NewServerResponse()
	s.globals["response"] = resp.Struct()
	return resp
}
