package worker

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/1set/starbox"
	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv"
	"github.com/psanford/memfs"
	"go.starlark.net/starlark"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type wireRequest struct {
	Protocol int `json:"protocol"`
	Request
}
type wireResponse struct {
	Protocol int             `json:"protocol"`
	OK       bool            `json:"ok"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
	Kind     string          `json:"kind,omitempty"`
}

func decodeOne(data []byte, value interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return fmt.Errorf("%w: extra or malformed message", ErrProtocol)
	}
	return nil
}
func validateRequest(request Request, limit int64) error {
	remaining := limit
	add := func(s string) error {
		if !utf8.ValidString(s) {
			return fmt.Errorf("%w: invalid UTF-8", ErrProtocol)
		}
		if int64(len(s)) > remaining {
			return ErrInputLimit
		}
		remaining -= int64(len(s))
		return nil
	}
	if err := add(request.Source); err != nil {
		return err
	}
	if err := add(request.Filename); err != nil {
		return err
	}
	for name, source := range request.Modules {
		if !fs.ValidPath(name) || !strings.HasSuffix(name, ".star") {
			return fmt.Errorf("%w: module must be a relative .star path", ErrProtocol)
		}
		for _, builtin := range starlet.GetAllBuiltinModuleNames() {
			if name == builtin+".star" {
				return fmt.Errorf("%w: reserved module name", ErrProtocol)
			}
		}
		if err := add(name); err != nil {
			return err
		}
		if err := add(source); err != nil {
			return err
		}
	}
	return nil
}

// Serve reads exactly one bounded protocol-v1 request and writes one response.
// It accepts no host filesystem or Go callbacks. The caller must supervise this
// process; byte quotas do not bound the child's intermediate encoding memory.
func Serve(in io.Reader, out, diagnostics io.Writer, maxInput int64) error {
	if maxInput <= 0 || maxInput == int64(^uint64(0)>>1) {
		return fmt.Errorf("%w: invalid input budget", ErrProtocol)
	}
	data, err := io.ReadAll(io.LimitReader(in, maxInput+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxInput {
		return ErrInputLimit
	}
	request := wireRequest{}
	if err := decodeOne(data, &request); err != nil {
		return err
	}
	if request.Protocol != 1 {
		return fmt.Errorf("%w: unsupported version", ErrProtocol)
	}
	if err := validateRequest(request.Request, maxInput); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(execute(request.Request, diagnostics))
}
func execute(request Request, diagnostics io.Writer) wireResponse {
	response := wireResponse{Protocol: 1}
	fail := func(kind string, err error) wireResponse {
		response.Kind = kind
		response.Error = err.Error()
		return response
	}
	box := starbox.NewWithPolicy("worker", starbox.Policy{Modules: starbox.ModuleAllow{Names: []string{"log"}}})
	box.SetModuleSet(starbox.EmptyModuleSet)
	box.AddNamedModules("log")
	box.SetPrintFunc(func(_ *starlark.Thread, message string) { _, _ = fmt.Fprintln(diagnostics, message) })
	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(diagnostics), zap.DebugLevel))
	box.SetLogger(logger.Sugar())
	memory := memfs.New()
	names := make([]string, 0, len(request.Modules))
	for name := range request.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if slash := strings.LastIndex(name, "/"); slash >= 0 {
			if err := memory.MkdirAll(name[:slash], 0700); err != nil {
				return fail("module", err)
			}
		}
		if err := memory.WriteFile(name, []byte(request.Modules[name]), 0600); err != nil {
			return fail("module", err)
		}
	}
	box.SetFS(memory)
	box.AddResultBuiltin("output")
	box.GetMachine().SetOutputConversionEnabled(false)
	filename := request.Filename
	if filename == "" {
		filename = "worker.star"
	}
	source := request.Source
	if source == "" {
		source = "\n"
	}
	if _, err := box.CreateRunConfig().FileName(filename).Script(source).Execute(); err != nil {
		return fail(starbox.ClassifyRunError(err).Kind.String(), err)
	}
	if value, set := box.GetResult(); set {
		result, err := dataconv.EncodeStarlarkJSON(value)
		if err != nil {
			return fail("result", err)
		}
		response.Result = json.RawMessage(result)
	}
	response.OK = true
	return response
}

// Main is the command's actual noninteractive I/O boundary. Build the executable
// from the same reviewed module; protocol=1 is not a module-version claim.
func Main(args []string, in io.Reader, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("starbox-worker", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	protocol := flags.Int("protocol", 0, "required protocol version")
	input := flags.Int64("max-input", 0, "required request byte limit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *protocol != 1 || *input <= 0 || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(diagnostics, "protocol=1 and a positive max-input are required")
		return 2
	}
	if err := Serve(in, out, diagnostics, *input); err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 2
	}
	return 0
}
