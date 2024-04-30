# :takeout_box: Starbox - Another Starlark runtime in a box

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
