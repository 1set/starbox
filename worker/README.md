# Single-request worker

This opt-in package launches a trusted worker executable for each request.
Build the command from the same reviewed checkout:

    go build -o /absolute/trusted/path/starbox-worker ./cmd/starbox-worker

Use positive budgets and an explicit executable path:

    runner, err := worker.New(worker.Config{
        Executable: "/absolute/trusted/path/starbox-worker",
        Timeout: 2 * time.Second,
        MaxRequestBytes: 1 << 20,
        MaxOutputBytes: 64 << 10,
    })
    // Check err before calling Run.
    result, err := runner.Run(context.Background(), worker.Request{
        Source: "print(\"hello\"); output({\"ok\": True})",
    })

The request limit counts the encoded protocol message, including source modules.
The output limit counts stdout and stderr together, including result framing and
error/fatal diagnostics. Exactly the limit is allowed; the first excess byte
causes termination and an ErrOutputLimit result. Retained output never exceeds
the limit. A failed or truncated response does not expose a successful Payload.

Each worker accepts one protocol-v1 request on stdin and exits. print and log
write to stderr; stdout carries the structured completion and optional JSON
result from output(value). Functions and other JSON-incompatible results fail
explicitly. A script error is a *RemoteError, distinct from a crash, malformed
protocol or external deadline. No arbitrary Go globals, callbacks, module
loaders or host filesystem cross the protocol.

Source modules are a finite map of relative .star paths in a new memory
filesystem. Only log is enabled as a builtin module; this is not the existing
Safe or Full module profile. Unknown loads cannot fall back to the host's
working directory. Each request has a private working directory and an empty
environment except required Windows SystemRoot.

The supervisor kills the direct worker when the external deadline/cancellation
or captured-output limit fires, waits for it exactly once, closes its parent
pipe endpoints and joins the I/O goroutines before returning. Reaped and
PipesClosed report that cleanup. Output ordering across the two streams is
unspecified; a byte limit can split UTF-8 diagnostics.

This does not limit CPU usage or RSS, contain descendants, restrict the entire
OS filesystem/network, or provide an untrusted multi-tenant sandbox. A child can
allocate while constructing a result before emitting any bytes. A kernel task
that cannot be interrupted may delay Wait. The trusted executable path must be
immutable to the host's threat model; protocol=1 is not proof of binary origin.

Existing starbox.New, Run and RunnerConfig APIs keep their in-process behavior.
Go 1.19 and the fixed interpreter/module requirements remain unchanged. No
runtime downloading, module-version resolution or automatic PATH discovery is
performed.

