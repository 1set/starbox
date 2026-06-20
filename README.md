# :takeout_box: Starbox - Unboxing the Potential of Starlark

[![godoc](https://pkg.go.dev/badge/github.com/1set/starbox.svg)](https://pkg.go.dev/github.com/1set/starbox)
[![codecov](https://codecov.io/github/1set/starbox/graph/badge.svg?token=8v1rqUSOfD)](https://codecov.io/github/1set/starbox)
[![codacy](https://app.codacy.com/project/badge/Grade/c706bea001fa48d3a958f609c7463200)](https://app.codacy.com/gh/1set/starbox/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade)
[![codeclimate](https://api.codeclimate.com/v1/badges/23baa9f82df1c504d2da/maintainability)](https://codeclimate.com/github/1set/starbox/maintainability)
[![go report](https://goreportcard.com/badge/github.com/1set/starbox)](https://goreportcard.com/report/github.com/1set/starbox)

*Starbox* is a pragmatic Go wrapper around the [*Starlark in Go*](https://github.com/google/starlark-go) project, making it easier to execute Starlark scripts, exchange data between Go and Starlark, and call functions across the Go-Starlark boundary. With a focus on simplicity and usability, *Starbox* aims to provide an enhanced experience for developers integrating Starlark scripting into their Go applications.

## 🚀 Key Features

A host of powerful features are provided to supercharge your Starlark scripting experience:

- **Streamlined Script Execution**: Simplifies setting up and running Starlark scripts, offering a seamless interface for both script execution and interactive REPL sessions.
- **Efficient Data Interchange**: Enables robust and smooth data exchange between Go and Starlark, enhancing the interoperability and simplifying the integration process.
- **Versatile Module Management**: Extends Starlark's capabilities with a suite of built-in functions and the ability to load custom and override existing modules, covering functionalities from data processing to HTTP handling and file manipulation.
- **Cross-Language Function Calls:** Leverage the power of both languages by calling Go functions from Starlark and vice versa, creating powerful integrations.
- **Integrated HTTP Context**: Facilitates handling HTTP requests and responses within Starlark scripts, catering to web application development and server-side scripting.
- **Collective Memory Sharing**: Introduces a shared memory concept, enabling data sharing across different script executions and instances, fostering a more connected and dynamic scripting environment.
- **Advanced Scripting Tools:** Utilize features like REPL for interactive exploration and debugging, along with script caching for improved performance.

## 📦 Installation

To include `starbox` in your Go project, use the following command:

```bash
go get github.com/1set/starbox
```

## ⚙️ Usage

Here's a quick example of how you can use Starbox:

```go
import "github.com/1set/starbox"

// Define your box with global variables and modules
box := starbox.New("quick")
box.AddKeyValue("greet", func(name string) string {
    return fmt.Sprintf("Hello, %s!", name)
})
box.AddNamedModules("random")

// Run a Starlark script
script := starbox.HereDoc(`
    target = random.choice(["World", "Starlark", "Starbox"])
    text = greet(target)
    print("Starlark:", text)
    print(__modules__)
`)
res, err := box.Run(script)

// Check for errors and results
if err != nil {
    fmt.Println("Error executing script:", err)
    return
}
fmt.Println("Go:", res["text"].(string))
```

This may output:

```
[⭐|quick](15:50:27.677) Starlark: Hello, Starbox!
[⭐|quick](15:50:27.677) ["random"]
Go: Hello, Starbox!
```

## 🔒 Security Model & Capability Gating

Starbox is a **host runtime**: the Go host decides what an untrusted script may reach, and the script cannot widen that grant. The control surface has two distinct layers — Starbox implements the first; the second lives at the layer that constructs the modules.

### Load gate — which modules a script may `load()`

`New` selects modules through a four-tier module set (`EmptyModuleSet`, `SafeModuleSet`, `NetworkModuleSet`, `FullModuleSet`); `SafeModuleSet` is an explicit, hand-curated allowlist that can reach **neither the network nor the filesystem**.

For stricter, host-declared control, construct the box with a `Policy` — a Go-side, **default-deny** allowlist the script can neither read nor mutate:

```go
// Only "math" and "json" are loadable, no matter what the module set requests.
box := starbox.NewWithPolicy("sandboxed", starbox.Policy{
    Modules: starbox.ModuleAllow{Names: []string{"math", "json"}},
})
box.SetModuleSet(starbox.FullModuleSet) // requested set is INTERSECTED with the policy
```

- The **zero** `Policy{}` permits nothing — strict default-deny by construction.
- `ModuleAllow.Capabilities` is opt-in capability widening: a non-zero tier (e.g. `starlet.CapNetwork`) permits every builtin whose capability set is a subset of it.
- The gate covers **every named-module path** a script can load: builtin, custom (`AddModule*`), dynamic, and script modules (`AddModuleScript`, matched by their registered `.star` name).
- A withheld **builtin** surfaces a typed `ModuleWithheldError` (matchable via `errors.As`). A policy-denied **non-builtin** (custom/dynamic/script) module is simply absent — `load()` fails as "not found", so the sandbox is not told a host-private module exists but is forbidden.

> **`SetFS` is an explicit exception.** A host-mounted `fs.FS` is a raw filesystem grant with no module-name registry to match against, so it is **not** governed by the load gate. Under a restrictive policy, curate the mounted filesystem (or do not call `SetFS`).

### Exec gate — what a loaded module may *do* — is NOT in Starbox

Per-call filesystem / network / command / secret gating (what a *loaded* module is allowed to actually do) is **out of scope for Starbox**: it never imports the domain modules, which arrive as opaque loaders, so exec-gating belongs where those loaders are constructed (the host shell / CLI). Starbox ships the load gate only; it does not ship inert exec-grant fields that would be a fail-open footgun.

## 📏 Execution Budgets & Output Limits

```go
box.SetMaxExecutionSteps(1_000_000) // bound runaway loops a wall-clock timeout cannot stop
box.SetMaxOutputEntries(100)        // cap the number of top-level result entries
```

A run that exceeds the step budget fails with a `starlet.MaxStepsExceededError`; one that produces too many result entries is withheld with an `OutputLimitExceededError`. Both are reachable via `errors.As`, and both are enforced on **every** run path (`Run`, `RunFile`, `RunTimeout`, `RunInspect*`, and the `RunnerConfig.Execute()` builder).

## 🔎 Inspection without Execution

Learn what a script *would* see, and catch problems, without running it:

```go
diags, _ := box.Check(script)            // []Diagnostic: syntax + resolve errors as "file:line:col: msg"
surface, _ := box.DescribeSurface()      // Surface: modules (name/origin/members) + globals (name/type)
```

Both honor the active `Policy` — they report and accept exactly the modules a real `Run` would load, never a wider surface.

## 📤 Structured Results & Console Capture

```go
box.AddResultBuiltin("output")           // script calls output(v) once per run to set its result
res, _ := box.Run(`output({"ok": True})`)
val, ok := box.GetResult()               // the captured value; reset at the start of every run

con := box.EnableConsoleCapture()        // funnel console output into a drainable buffer instead of stderr
box.Run(`print("hi")`)
for _, e := range con.Drain() {          // []ConsoleEntry: Time, Level, Message, structured Fields
    fmt.Println(e.Level, e.Message, e.Fields)
}
```

`print()` becomes a `LevelPrint` entry; when the `log` module is loaded, `log.*` calls become leveled entries whose keyword arguments are preserved as structured `Fields` (never pre-rendered into the message). `Drain` returns the buffered entries and clears them, for a per-run drain.

## 🧩 Typed Errors

Run failures carry typed, `errors.As`-matchable causes, and `ClassifyRunError` maps any run failure to a `RunError{Kind}` (`Syntax`, `Compile`, `ModuleWithheld`, `MaxSteps`, `OutputLimit`, `Eval`) for uniform host handling.

## 👥 Contributing

We welcome contributions to the *Starbox* project. If you encounter any issues or have suggestions for improvements, please feel free to open an issue or submit a pull request. Before undertaking any significant changes, please let us know by filing an issue or claiming an existing one to ensure there is no duplication of effort.

## 📜 License

*Starbox* is licensed under the [MIT License](LICENSE).

## 🙌 Credits

This project is inspired by and builds upon several open-source projects:

- [Starlark in Go](https://github.com/google/starlark-go): The official Starlark interpreter in Go, created by Google.
- [Starlight](https://github.com/starlight-go/starlight): A well-known Go wrapper and data conversion tool between Go and Starlark.
- [Starlight Enhanced](https://github.com/1set/starlight): A sophisticated fork of the original Starlight, with bug fixes and enhancement features.
- [Starlib](https://github.com/qri-io/starlib): A collection of third-party libraries for Starlark.
- [Starlet](https://github.com/1set/starlet): A Go wrapper that simplifies usage, offers data conversion, libraries and extensions for Starlark.

We thank the authors and contributors of these projects for their excellent works 🎉
