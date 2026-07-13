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
	"go.uber.org/zap"
)

// DoNotCompare prevents == and != comparisons on the containing struct.
type DoNotCompare [0]func()

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
	_                DoNotCompare
	mac              *starlet.Machine
	mu               sync.RWMutex
	hasExec          bool
	execTimes        uint
	name             string
	structTag        string
	printFunc        starlet.PrintFunc
	globals          starlet.StringAnyMap
	modSet           ModuleSetName
	namedMods        []string
	loadMods         starlet.ModuleLoaderMap
	modMembers       map[string][]string
	scriptMods       map[string]string
	modFS            fs.FS
	modNames         []string
	dynMods          DynamicModuleLoader
	userLog          *zap.SugaredLogger
	result           starlark.Value
	resultSet        bool
	maxOutputEntries uint
	maxSteps         uint64
	scriptCache      starlet.ByteCache
	scriptCacheSet   bool
	policy           *Policy
	console          *Console
}

// New creates a new Starbox instance with default settings.
func New(name string) *Starbox {
	return &Starbox{mac: newStarMachine(name), name: name}
}

// NewWithPolicy creates a Starbox whose loadable modules are constrained by a
// host-side, default-deny Policy (the A4 load gate). The policy is deep-copied
// in (the caller cannot mutate grants afterwards) and only ever TIGHTENS what
// SetModuleSet/AddNamedModules/custom/dynamic modules would otherwise load — a
// module loads iff it is both requested AND permitted by the policy. Modules
// the policy withholds raise a ModuleWithheldError (for builtins) or are simply
// absent (custom/dynamic). New (no policy) behaviour is unchanged.
func NewWithPolicy(name string, p Policy) *Starbox {
	s := New(name)
	cp := p.clone()
	s.policy = &cp
	return s
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

// deniedAfterExec reports whether a configuration change must be rejected
// because the Box has already executed. It logs the misuse through the package
// logger (which panics under a development logger, per the setters' contract)
// and returns true so the caller bails out WITHOUT applying the change. The
// return is what enforces the contract fail-closed: the default logger is a
// no-op zap logger whose DPanic neither panics nor logs, so before this the
// guard was silent and the change landed anyway. The caller holds s.mu.
func (s *Starbox) deniedAfterExec(action string) bool {
	if s.hasExec {
		log.DPanic("cannot " + action + " after execution")
		return true
	}
	return false
}

// String returns the name of the Starbox instance.
func (s *Starbox) String() string {
	return fmt.Sprintf("🥡Box{name:%s,run:%d}", s.name, s.execTimes)
}

// Reset replaces the underlying Starlet machine with a fresh one while keeping
// the Box's configuration (name, globals, module set, script modules, policy,
// limits, ...), so the same Box can be run again from a clean per-run state.
//
// Reset is for SERIAL reuse of a single Box. There is deliberately no Clone and
// no Box pool: per-run state (globals injected during a run, the Starlark step
// counter) lives on the machine, so sharing or hand-pooling a Box would leak
// that state between runs. For a hot path or concurrent workload, construct a
// fresh New(...) per run and share only the compiled-program cache across them
// via SetScriptCache - that keeps compilation shared while keeping per-run state
// isolated.
func (s *Starbox) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	//s.mac.Reset()
	s.mac = newStarMachine(s.name)
	s.hasExec = false
	// Re-apply the Box-level limits/cache to the fresh machine eagerly, not
	// lazily at the next run: newStarMachine defaults to no step budget and an
	// enabled cache, so without this a caller that reaches for the raw machine
	// via GetMachine right after Reset would see the guard dropped. This keeps
	// Reset's "keeps the Box's limits" contract true for the machine itself.
	s.mac.SetMaxExecutionSteps(s.maxSteps)
	s.applyScriptCache()
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

// SetMaxExecutionSteps sets the per-run budget of Starlark computation steps;
// 0 (the default) means unlimited. When a run exceeds the budget, Run/Call fail
// with a starlet.MaxStepsExceededError reachable via errors.As — the standard
// guard against a runaway loop that a wall-clock timeout cannot stop. The step
// counter resets at the start of every run. Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) SetMaxExecutionSteps(steps uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set max execution steps") {
		return
	}
	// Store on the Box, not only on the current machine: Reset() swaps in a
	// fresh machine (which defaults to unlimited), so a machine-only budget
	// vanished after the first run — dropping the CPU-DoS guard on exactly the
	// serial-reuse path Reset exists for. Reset re-applies s.maxSteps to the
	// fresh machine, the same way maxOutputEntries survives Reset.
	s.maxSteps = steps
	s.mac.SetMaxExecutionSteps(steps)
}

// SetMaxOutputEntries sets the maximum number of top-level entries a run's
// result may contain; 0 (the default) means unlimited. A run that produces more
// is aborted with an OutputLimitExceededError (reachable via errors.As) and its
// result is withheld. This is a post-hoc policy gate on result size, not a
// memory guard - use SetMaxExecutionSteps to bound resource use. Calling it
// after execution is rejected: the change is ignored, and it panics under a
// development logger.
func (s *Starbox) SetMaxOutputEntries(n uint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set max output entries") {
		return
	}
	s.maxOutputEntries = n
}

// GetModuleNames returns the names of the modules loaded after execution.
func (s *Starbox) GetModuleNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.modNames
}

// SetLogger sets the logger for user-defined log output.
func (s *Starbox) SetLogger(sl *zap.SugaredLogger) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set logger") {
		return
	}
	s.userLog = sl
}

// SetStructTag sets the custom tag of Go struct fields for Starlark.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) SetStructTag(tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set tag") {
		return
	}
	s.structTag = tag
}

// SetPrintFunc sets the print function for Starlark.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) SetPrintFunc(printFunc starlet.PrintFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set print function") {
		return
	}
	s.printFunc = printFunc
}

// SetFS sets the virtual filesystem for module scripts.
// If it's not nil, it'll override all the scripts added by AddModuleScript().
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) SetFS(hfs fs.FS) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set filesystem") {
		return
	}
	s.modFS = hfs
}

// SetScriptCache sets a custom cache provider for compiled script content; a
// nil provider disables the script cache. Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
//
// Hot-path / concurrency recommendation: a single starlet.MemoryCache
// (NewMemoryCache, which is safe for concurrent use) shared across many per-run
// New(...) boxes lets each distinct script compile once and be reused, while
// every Box keeps its own isolated per-run state. Prefer this over reusing or
// pooling a single Box (see Reset).
func (s *Starbox) SetScriptCache(cache starlet.ByteCache) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set script cache") {
		return
	}
	// Store on the Box too, so the choice survives Reset: newStarMachine always
	// enables the cache, so a machine-only SetScriptCache(nil) was re-enabled
	// after Reset. Reset re-applies the stored choice to the fresh machine.
	s.scriptCache = cache
	s.scriptCacheSet = true
	s.applyScriptCache()
}

// applyScriptCache applies the Box's script-cache choice to the current
// machine. The caller holds s.mu.
func (s *Starbox) applyScriptCache() {
	if !s.scriptCacheSet {
		return
	}
	if s.scriptCache == nil {
		s.mac.SetScriptCacheEnabled(false)
	} else {
		s.mac.SetScriptCache(s.scriptCache)
	}
}

// SetDynamicModuleLoader sets the dynamic module loader for preload and lazyload modules.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) SetDynamicModuleLoader(loader DynamicModuleLoader) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set dynamic module loader") {
		return
	}
	s.dynMods = loader
}

// SetModuleSet sets the module set to be loaded before execution.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) SetModuleSet(modSet ModuleSetName) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("set module set") {
		return
	}
	s.modSet = modSet
}

// AddKeyValue adds a key-value pair to the global environment before execution.
// If the key already exists, it will be overwritten.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddKeyValue(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add key-value pair") {
		return
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals[key] = value
}

// AddKeyStarlarkValue adds a key-value pair to the global environment before execution, the value is a Starlark value.
// If the key already exists, it will be overwritten.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddKeyStarlarkValue(key string, value starlark.Value) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add key-value pair") {
		return
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals[key] = value
}

// AddKeyValues adds key-value pairs to the global environment before execution. Usually for output of Run()*.
// For each key-value pair, if the key already exists, it will be overwritten.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddKeyValues(keyValues starlet.StringAnyMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add key-value pairs") {
		return
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals.Merge(keyValues)
}

// AddStarlarkValues adds key-value pairs to the global environment before execution, the values are already converted to Starlark values.
// For each key-value pair, if the key already exists, it will be overwritten.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddStarlarkValues(keyValues starlark.StringDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add key-value pairs") {
		return
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
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddBuiltin(name string, starFunc StarlarkFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add builtin") {
		return
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	sb := starlark.NewBuiltin(name, starFunc)
	s.globals[name] = sb
}

// AddNamedModules adds builtin and custom modules by name to the preload and lazyload registry.
// It will not load the modules until the first run.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddNamedModules(moduleNames ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add named modules") {
		return
	}
	s.namedMods = append(s.namedMods, moduleNames...)
}

// AddModulesByName is an alias of AddNamedModules().
func (s *Starbox) AddModulesByName(moduleNames ...string) {
	s.AddNamedModules(moduleNames...)
}

// AddModuleLoader adds a custom module loader to the preload and lazyload registry.
// It will not load the module until the first run, and load result can be accessed in script via load("module_name", "key1") or key1 directly.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddModuleLoader(moduleName string, moduleLoader starlet.ModuleLoader) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add module loader") {
		return
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	s.loadMods[moduleName] = moduleLoader
}

// AddModuleFunctions adds a module with the given module functions along with a module loader, and adds it to the preload and lazyload registry.
// The given module function can be accessed in script via load("module_name", "func1") or module_name.func1.
// It works like AddModuleData() but allows only functions as values.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddModuleFunctions(name string, funcs FuncMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add module function") {
		return
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	sfd := starlark.StringDict{}
	for fn, fv := range funcs {
		sfd[fn] = starlark.NewBuiltin(name+"."+fn, fv)
	}
	s.loadMods[name] = dataconv.WrapModuleData(name, sfd)
	s.recordModMembers(name, sfd)
}

// AddModuleData creates a module for the given module data along with a module loader, and adds it to the preload and lazyload registry.
// The given module data can be accessed in script via load("module_name", "key1") or module_name.key1.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddModuleData(moduleName string, moduleData starlark.StringDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add module data") {
		return
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	s.loadMods[moduleName] = dataconv.WrapModuleData(moduleName, moduleData)
	s.recordModMembers(moduleName, moduleData)
}

// AddStructFunctions adds a module with the given struct functions along with a module loader, and adds it to the preload and lazyload registry.
// The given struct function can be accessed in script via load("struct_name", "func1") or struct_name.func1.
// It works like AddStructData() but allows only functions as values.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddStructFunctions(name string, funcs FuncMap) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add struct function") {
		return
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	sfd := starlark.StringDict{}
	for fn, fv := range funcs {
		sfd[fn] = starlark.NewBuiltin(name+"."+fn, fv)
	}
	s.loadMods[name] = dataconv.WrapStructData(name, sfd)
	s.recordModMembers(name, sfd)
}

// AddStructData creates a module for the given struct data along with a module loader, and adds it to the preload and lazyload registry.
// The given struct data can be accessed in script via load("struct_name", "key1") or struct_name.key1.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddStructData(structName string, structData starlark.StringDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add struct data") {
		return
	}
	if s.loadMods == nil {
		s.loadMods = make(map[string]starlet.ModuleLoader)
	}
	s.loadMods[structName] = dataconv.WrapStructData(structName, structData)
	s.recordModMembers(structName, structData)
}

// AddModuleScript creates a module with given module script in virtual filesystem, and adds it to the preload and lazyload registry.
// The given module script can be accessed in script via load("module_name", "key1") or load("module_name.star", "key1") if module name has no ".star" suffix.
// All the module scripts added by this method would be overridden by SetFS() if it's not nil.
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddModuleScript(moduleName, moduleScript string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add module script") {
		return
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
// Calling it after execution is rejected: the change is ignored, and it panics under a development logger.
func (s *Starbox) AddHTTPContext(req *http.Request) *libhttp.ServerResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deniedAfterExec("add HTTP context") {
		return nil
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
