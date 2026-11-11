package worker

// Sections: public protocol I/O; command arguments; result/module errors.
import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("fixture write failure") }

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("fixture read failure") }

func TestServeProtocolIO(t *testing.T) {
	for _, wire := range []string{
		"{\"protocol\":1,\"source\":\"output(7)\"}",
		"{\"protocol\":1,\"source\":\"load(\\\"m.star\\\",\\\"v\\\"); output(v)\",\"modules\":{\"m.star\":\"v=7\"}}",
	} {
		var out, diagnostics bytes.Buffer
		if err := Serve(strings.NewReader(wire), &out, &diagnostics, 4096); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\"result\":7") {
			t.Fatalf("out=%s diagnostics=%s", &out, &diagnostics)
		}
	}
	for _, wire := range []string{
		"bad", "{\"protocol\":2,\"source\":\"x=1\"}",
		"{\"protocol\":1,\"source\":\"x=1\",\"unknown\":true}",
		"{\"protocol\":1,\"source\":\"x=1\"} {}",
		"{\"protocol\":1,\"modules\":{\"../x.star\":\"x=1\"}}",
	} {
		if err := Serve(strings.NewReader(wire), io.Discard, io.Discard, 4096); err == nil {
			t.Fatalf("accepted %s", wire)
		}
	}
	if err := Serve(strings.NewReader(strings.Repeat(" ", 9)), io.Discard, io.Discard, 8); err == nil {
		t.Fatal("input cap missed")
	}
	if err := Serve(failedReader{}, io.Discard, io.Discard, 16); err == nil {
		t.Fatal("reader failure missed")
	}
	if err := Serve(strings.NewReader("{\"protocol\":1,\"source\":\"x=1\"}"), failedWriter{}, io.Discard, 4096); err == nil {
		t.Fatal("writer failure missed")
	}
	if err := Serve(strings.NewReader(""), io.Discard, io.Discard, 0); err == nil {
		t.Fatal("zero cap accepted")
	}
}

func TestMainIO(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if status := Main([]string{"--protocol=1", "--max-input=4096"}, strings.NewReader("{\"protocol\":1,\"source\":\"output(7)\"}"), &out, &diagnostics); status != 0 {
		t.Fatalf("status=%d diagnostics=%s", status, &diagnostics)
	}
	for _, args := range [][]string{{}, {"--protocol=2", "--max-input=8"}, {"--protocol=1"}, {"--unknown"}, {"--protocol=1", "--max-input=8", "extra"}} {
		if Main(args, strings.NewReader("{}"), io.Discard, io.Discard) == 0 {
			t.Fatalf("invalid args %v", args)
		}
	}
	if Main([]string{"--protocol=1", "--max-input=8"}, strings.NewReader("bad"), io.Discard, io.Discard) == 0 {
		t.Fatal("invalid request accepted")
	}
}

func TestServeResultAndModuleErrors(t *testing.T) {
	for _, source := range []string{"x = ???", "fail(\"expected\")", "output(lambda x:x)", "load(\"file\",\"read_string\")"} {
		wire, _ := json.Marshal(map[string]interface{}{"protocol": 1, "source": source})
		var out bytes.Buffer
		if err := Serve(bytes.NewReader(wire), &out, io.Discard, 4096); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\"ok\":false") || !strings.Contains(out.String(), "\"error\":") {
			t.Fatalf("failure disguised: %s", &out)
		}
	}
}

func TestServeDiagnosticsAndNestedModules(t *testing.T) {
	request := wireRequest{Protocol: 1, Request: Request{Source: "print(\"printed\"); log.info(\"logged\"); load(\"lib/m.star\",\"v\"); output(v)", Filename: "label.star", Modules: map[string]string{"lib/m.star": "v=7"}}}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if err := Serve(bytes.NewReader(data), &out, &diagnostics, 4096); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"result\":7") || !strings.Contains(diagnostics.String(), "printed") || !strings.Contains(diagnostics.String(), "logged") {
		t.Fatalf("out=%s diagnostics=%s", &out, &diagnostics)
	}
	// A supplied file cannot also be the directory of another source module.
	request = wireRequest{Protocol: 1, Request: Request{Source: "x=1", Modules: map[string]string{"a.star": "x=1", "a.star/b.star": "x=2"}}}
	data, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Serve(bytes.NewReader(data), &out, io.Discard, 4096); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"kind\":\"module\"") {
		t.Fatalf("module conflict hidden: %s", &out)
	}
}

func TestEmptySourceIsNotAFileRequest(t *testing.T) {
	var out bytes.Buffer
	wire := "{\"protocol\":1,\"source\":\"\",\"filename\":\"missing.star\"}"
	if err := Serve(strings.NewReader(wire), &out, io.Discard, 4096); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"protocol\":1,\"ok\":true}\n" {
		t.Fatalf("empty source attempted a file load: %s", &out)
	}
}
