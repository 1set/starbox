package worker

// Sections: admission; protocol/output quota; process/fatal isolation; child helper.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"
	"time"
)

const testResponse = "{\"protocol\":1,\"ok\":true}\n"

func helperRunner(t *testing.T, mode string) *Runner {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(Config{Executable: executable, Timeout: 5 * time.Second, MaxRequestBytes: 1 << 20, MaxOutputBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	r.args = []string{"-test.run=^TestWorkerHelper$"}
	r.env = append(r.env, "STARBOX_WORKER_HELPER="+mode, "GORACE=atexit_sleep_ms=0", "GOCOVERDIR="+t.TempDir())
	return r
}

func assertReaped(t *testing.T, result Result) {
	t.Helper()
	if !result.Reaped || !result.PipesClosed || result.PID <= 0 {
		t.Fatalf("not reaped/closed: pid=%d reaped=%v closed=%v", result.PID, result.Reaped, result.PipesClosed)
	}
	t.Logf("pid=%d exit=%d reaped=%v pipesClosed=%v captured=%d truncated=%v", result.PID, result.ExitCode, result.Reaped, result.PipesClosed, len(result.Stdout)+len(result.Stderr), result.OutputTruncated)
	if runtime.GOOS == "windows" {
		return
	}
	p, err := os.FindProcess(result.PID)
	if err != nil {
		return
	}
	defer p.Release()
	if err := p.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("worker %d still present", result.PID)
	}
}

func TestConfigAdmission(t *testing.T) {
	valid := Config{Executable: os.Args[0], Timeout: time.Second, MaxRequestBytes: 1024, MaxOutputBytes: 1024}
	for name, change := range map[string]func(*Config){
		"relative": func(c *Config) { c.Executable = "worker" },
		"timeout":  func(c *Config) { c.Timeout = 0 },
		"input":    func(c *Config) { c.MaxRequestBytes = 0 },
		"output":   func(c *Config) { c.MaxOutputBytes = 0 },
		"overflow": func(c *Config) { c.MaxRequestBytes = int64(^uint64(0) >> 1) },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			change(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestRequestAdmission(t *testing.T) {
	r := helperRunner(t, "serve")
	for _, request := range []Request{
		{Source: strings.Repeat("x", 1<<20)},
		{Source: "x=1", Modules: map[string]string{"../x.star": "x=1"}},
		{Source: "x=1", Modules: map[string]string{"bad": "x=1"}},
		{Source: "x=1", Modules: map[string]string{"log.star": "x=1"}},
	} {
		result, err := r.Run(context.Background(), request)
		if err == nil || result.PID != 0 {
			t.Fatalf("invalid request started: pid=%d err=%v", result.PID, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := r.Run(ctx, Request{Source: "x=1"})
	if !errors.Is(err, context.Canceled) || result.PID != 0 {
		t.Fatalf("cancel admission: %v", err)
	}
	if _, err := r.Run(nil, Request{}); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestRunProtocol(t *testing.T) {
	r := helperRunner(t, "serve")
	for _, tc := range []struct {
		name, source string
		modules      map[string]string
		want         string
		failure      bool
	}{
		{"result", "print(\"hi\"); output({\"n\":7})", nil, "{\"n\":7}", false},
		{"none", "output(None)", nil, "null", false},
		{"no_result", "x=7", nil, "", false},
		{"empty", "", nil, "", false},
		{"load", "load(\"m.star\",\"n\"); output(n)", map[string]string{"m.star": "n=7"}, "7", false},
		{"syntax", "x = ???", nil, "", true},
		{"eval", "fail(\"expected\")", nil, "", true},
		{"result_type", "output(lambda x:x)", nil, "", true},
		{"log", "log.info(\"logged\"); output(True)", nil, "true", false},
		{"missing_load", "load(\"missing.star\",\"n\")", nil, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := r.Run(context.Background(), Request{Source: tc.source, Modules: tc.modules})
			assertReaped(t, result)
			if (err != nil) != tc.failure {
				t.Fatalf("err=%v stderr=%s", err, result.Stderr)
			}
			if tc.failure {
				var remote *RemoteError
				if !errors.As(err, &remote) || remote.Kind == "" || !strings.Contains(remote.Error(), remote.Message) {
					t.Fatalf("untyped remote failure: %v", err)
				}
			}
			if !tc.failure && string(result.Payload) != tc.want {
				t.Fatalf("payload=%s want=%s", result.Payload, tc.want)
			}
			if tc.name == "result" && !bytes.Contains(result.Stderr, []byte("hi")) {
				t.Fatal("print missing")
			}
			if tc.name == "log" && !bytes.Contains(result.Stderr, []byte("logged")) {
				t.Fatal("log missing")
			}
		})
	}
}

func TestCombinedOutputBoundary(t *testing.T) {
	total := int64(len(testResponse) + len("é"))
	for _, limit := range []int64{total, total - 1} {
		r := helperRunner(t, "boundary")
		r.config.MaxOutputBytes = limit
		result, err := r.Run(context.Background(), Request{})
		assertReaped(t, result)
		if int64(len(result.Stdout)+len(result.Stderr)) > limit {
			t.Fatal("combined quota exceeded")
		}
		if limit == total {
			if err != nil || result.OutputTruncated {
				t.Fatalf("exact boundary: %v", err)
			}
		} else {
			if !errors.Is(err, ErrOutputLimit) || !result.OutputTruncated || result.Payload != nil {
				t.Fatalf("over boundary: %v", err)
			}
		}
	}
	r := helperRunner(t, "flood")
	r.config.MaxOutputBytes = 97
	result, err := r.Run(context.Background(), Request{})
	assertReaped(t, result)
	if !errors.Is(err, ErrOutputLimit) || len(result.Stdout)+len(result.Stderr) != 97 {
		t.Fatalf("flood bytes=%d err=%v", len(result.Stdout)+len(result.Stderr), err)
	}
}

func TestNoncooperativeChildStopsAndClosesInput(t *testing.T) {
	for i := 0; i < 2; i++ {
		r := helperRunner(t, "stall")
		r.config.Timeout = time.Second
		start := time.Now()
		result, err := r.Run(context.Background(), Request{Source: strings.Repeat(" ", 128<<10)})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want deadline: %v", err)
		}
		assertReaped(t, result)
		if !bytes.Contains(result.Stderr, []byte("READY")) {
			t.Fatal("child not ready")
		}
		if time.Since(start) > 4*time.Second {
			t.Fatal("cleanup exceeded outer bound")
		}
	}
	r := helperRunner(t, "serve")
	result, err := r.Run(context.Background(), Request{Source: "output(9)"})
	assertReaped(t, result)
	if err != nil || string(result.Payload) != "9" {
		t.Fatalf("successor: %v", err)
	}
}

func TestParserFatalIsContained(t *testing.T) {
	deep := "x = " + strings.Repeat("(", 12000) + "0" + strings.Repeat(")", 12000)
	for _, loaded := range []bool{false, true} {
		r := helperRunner(t, "small-stack")
		request := Request{Source: deep}
		if loaded {
			request = Request{Source: "load(\"deep.star\",\"x\")", Modules: map[string]string{"deep.star": deep}}
		}
		result, err := r.Run(context.Background(), request)
		assertReaped(t, result)
		if err == nil || result.ExitCode == 0 || !bytes.Contains(result.Stderr, []byte("stack overflow")) {
			t.Fatalf("not parser fatal: err=%v exit=%d stderr bytes=%d", err, result.ExitCode, len(result.Stderr))
		}
		prefix := result.Stderr
		if len(prefix) > 300 {
			prefix = prefix[:300]
		}
		t.Logf("actual fatal loaded=%v diagnostic-prefix=%q", loaded, prefix)
	}
	r := helperRunner(t, "serve")
	result, err := r.Run(context.Background(), Request{Source: "output(\"alive\")"})
	assertReaped(t, result)
	if err != nil || string(result.Payload) != "\"alive\"" {
		t.Fatalf("successor: %v", err)
	}
}

func TestExitAndProtocolFailures(t *testing.T) {
	for _, mode := range []string{"exit", "bad-protocol", "empty"} {
		r := helperRunner(t, mode)
		result, err := r.Run(context.Background(), Request{})
		assertReaped(t, result)
		if err == nil {
			t.Fatalf("%s falsely successful", mode)
		}
		if mode != "exit" && !errors.Is(err, ErrProtocol) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	r := helperRunner(t, "serve")
	r.config.Executable = t.TempDir() + "/absent"
	result, err := r.Run(context.Background(), Request{})
	if err == nil || result.PID != 0 || result.Reaped {
		t.Fatalf("bad launch: %v", err)
	}
}

// Test-only helper: no debug/loop switches are present in the shipped command.
func TestWorkerHelper(t *testing.T) {
	mode := os.Getenv("STARBOX_WORKER_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "stall":
		fmt.Fprintln(os.Stderr, "READY")
		for {
			time.Sleep(time.Hour)
		}
	case "flood":
		for i := 0; i < 100000; i++ {
			fmt.Fprint(os.Stdout, "out")
			fmt.Fprint(os.Stderr, "err")
		}
		os.Exit(0)
	case "boundary":
		_, _ = io.Copy(io.Discard, os.Stdin)
		fmt.Fprint(os.Stderr, "é")
		fmt.Fprint(os.Stdout, testResponse)
		os.Exit(0)
	case "exit":
		os.Exit(7)
	case "empty":
		os.Exit(0)
	case "bad-protocol":
		fmt.Fprint(os.Stdout, "not json")
		os.Exit(0)
	case "no-input":
		fmt.Fprint(os.Stdout, testResponse)
		os.Exit(0)
	case "environment":
		_, _ = io.Copy(io.Discard, os.Stdin)
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(98)
		}
		payload, _ := json.Marshal(map[string]interface{}{"secret": os.Getenv("STARBOX_WORKER_TEST_SECRET") != "", "dir": dir})
		_ = json.NewEncoder(os.Stdout).Encode(wireResponse{Protocol: 1, OK: true, Result: payload})
		os.Exit(0)
	case "wrong-version", "extra-response", "contradictory", "missing-error", "unknown-response":
		_, _ = io.Copy(io.Discard, os.Stdin)
		responses := map[string]string{
			"wrong-version":    "{\"protocol\":2,\"ok\":true}",
			"extra-response":   testResponse + "{}",
			"contradictory":    "{\"protocol\":1,\"ok\":true,\"error\":\"bad\"}",
			"missing-error":    "{\"protocol\":1,\"ok\":false}",
			"unknown-response": "{\"protocol\":1,\"ok\":true,\"unknown\":true}",
		}
		fmt.Fprint(os.Stdout, responses[mode])
		os.Exit(0)
	case "small-stack":
		debug.SetMaxStack(256 << 10)
	case "serve":
	default:
		os.Exit(99)
	}
	os.Exit(Main([]string{"--protocol=1", "--max-input=1048576"}, os.Stdin, os.Stdout, os.Stderr))
}

func TestHostFilesAreNotLoaded(t *testing.T) {
	r := helperRunner(t, "serve")
	file := t.TempDir() + "/private.star"
	if err := os.WriteFile(file, []byte("fail(\"host file was read\")"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(context.Background(), Request{Filename: file})
	assertReaped(t, result)
	if err != nil {
		t.Fatalf("empty source treated as a filename: %v", err)
	}
	result, err = r.Run(context.Background(), Request{Source: fmt.Sprintf("load(%q,\"secret\")", file)})
	assertReaped(t, result)
	var remote *RemoteError
	if !errors.As(err, &remote) || strings.Contains(remote.Message, "host file was read") {
		t.Fatalf("host FS fallback: %v", err)
	}
}

func TestResponseContract(t *testing.T) {
	for _, mode := range []string{"wrong-version", "extra-response", "contradictory", "missing-error", "unknown-response"} {
		r := helperRunner(t, mode)
		result, err := r.Run(context.Background(), Request{})
		assertReaped(t, result)
		if !errors.Is(err, ErrProtocol) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}

func TestRequestTransportEdges(t *testing.T) {
	r := helperRunner(t, "serve")
	cases := []Request{
		{Source: string([]byte{0xff})},
		{Filename: string([]byte{0xff})},
		{Filename: strings.Repeat("n", 1<<20)},
		{Modules: map[string]string{"x.star": string([]byte{0xff})}},
		{Modules: map[string]string{"x.star": strings.Repeat(" ", 1<<20)}},
	}
	for _, request := range cases {
		result, err := r.Run(context.Background(), request)
		if err == nil || result.PID != 0 {
			t.Fatalf("bad request admitted: %v", err)
		}
	}
	r.config.MaxRequestBytes = 64
	result, err := r.Run(context.Background(), Request{Source: strings.Repeat(" ", 40), Modules: map[string]string{strings.Repeat("x", 30) + ".star": ""}})
	if !errors.Is(err, ErrInputLimit) || result.PID != 0 {
		t.Fatalf("module name budget: %v", err)
	}
	result, err = r.Run(context.Background(), Request{Source: strings.Repeat("\x01", 20)})
	if !errors.Is(err, ErrInputLimit) || result.PID != 0 {
		t.Fatalf("encoded quota: %v", err)
	}
	r = helperRunner(t, "no-input")
	result, err = r.Run(context.Background(), Request{Source: strings.Repeat(" ", 128<<10)})
	assertReaped(t, result)
	if err == nil || !strings.Contains(err.Error(), "pipe") {
		t.Fatalf("unconsumed request accepted: %v", err)
	}
	r = helperRunner(t, "serve")
	name := "TMPDIR"
	if runtime.GOOS == "windows" {
		name = "TMP"
		t.Setenv("TEMP", t.TempDir()+"/absent")
	}
	t.Setenv(name, t.TempDir()+"/absent")
	result, err = r.Run(context.Background(), Request{})
	if err == nil || result.PID != 0 {
		t.Fatalf("temp directory failure ignored: %v", err)
	}
}

func TestCleanupClosesRealBlockedPipes(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		for _, prior := range []error{nil, ErrOutputLimit} {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			readError := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, reader); readError <- err; close(done) }()
			ctx, cancel := context.WithCancel(context.Background())
			if cancelled {
				cancel()
			}
			start := time.Now()
			got := finishIO(ctx, done, func() { _ = reader.Close() }, prior)
			cancel()
			_ = writer.Close()
			if prior != nil {
				if !errors.Is(got, prior) {
					t.Fatalf("lost primary cause: %v", got)
				}
			} else if cancelled {
				if !errors.Is(got, context.Canceled) {
					t.Fatalf("cancel cleanup: %v", got)
				}
			} else if !errors.Is(got, ErrProtocol) {
				t.Fatalf("held pipe falsely complete: %v", got)
			}
			if err := <-readError; err == nil {
				t.Fatal("fixture reached EOF before parent close")
			}
			if time.Since(start) > time.Second {
				t.Fatal("blocked pipe cleanup did not join")
			}
		}
	}
}

func TestEnvironmentAndWorkingDirectory(t *testing.T) {
	t.Setenv("STARBOX_WORKER_TEST_SECRET", "synthetic-do-not-inherit")
	r := helperRunner(t, "environment")
	result, err := r.Run(context.Background(), Request{})
	assertReaped(t, result)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Secret bool
		Dir    string
	}
	if err := json.Unmarshal(result.Payload, &value); err != nil {
		t.Fatal(err)
	}
	if value.Secret || !strings.Contains(value.Dir, "starbox-worker-") {
		t.Fatalf("environment/directory boundary: %s", result.Payload)
	}
	if _, err := os.Stat(value.Dir); !os.IsNotExist(err) {
		t.Fatalf("per-run directory retained: %v", err)
	}
}
