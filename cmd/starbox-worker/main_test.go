package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunActualProtocolIO(t *testing.T) {
	var out, diagnostics bytes.Buffer
	status := run([]string{"--protocol=1", "--max-input=4096"}, strings.NewReader("{\"protocol\":1,\"source\":\"output(42)\"}"), &out, &diagnostics)
	if status != 0 || !strings.Contains(out.String(), "\"result\":42") {
		t.Fatalf("status=%d out=%s diagnostics=%s", status, &out, &diagnostics)
	}
}
