// Package worker supervises an explicitly supplied single-request executable.
// It bounds captured stdout/stderr and kills/reaps the direct child. It is not
// an OS sandbox and supplies no CPU/RSS quotas or descendant containment.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Sentinel errors identify transport limits and malformed responses.
var (
	ErrOutputLimit = errors.New("worker output byte limit exceeded")
	ErrInputLimit  = errors.New("worker request byte limit exceeded")
	ErrProtocol    = errors.New("invalid worker protocol")
)

// Config requires positive budgets and a trusted immutable absolute executable.
// The host builds it from reviewed source; protocol version is not authentication.
type Config struct {
	Executable                      string
	Timeout                         time.Duration
	MaxRequestBytes, MaxOutputBytes int64
}

// Request carries source, not host objects or callbacks. The caller must not
// mutate Modules during Run.
type Request struct {
	Source   string            `json:"source"`
	Filename string            `json:"filename,omitempty"`
	Modules  map[string]string `json:"modules,omitempty"`
}

// Result retains at most MaxOutputBytes across Stdout and Stderr, with no
// promised cross-stream ordering. Payload requires an intact successful reply.
// Reaped/PipesClosed describe this direct child and the joined parent I/O.
type Result struct {
	Stdout, Stderr                       []byte
	Payload                              json.RawMessage
	PID, ExitCode                        int
	Reaped, PipesClosed, OutputTruncated bool
}

// RemoteError reports a script or result-encoding failure. The child's original
// Go error chain cannot be transported by this JSON protocol.
type RemoteError struct{ Kind, Message string }

func (e *RemoteError) Error() string { return "worker " + e.Kind + ": " + e.Message }

// Runner is immutable after New; every concurrent Run creates a fresh child.
type Runner struct {
	config    Config
	args, env []string
}

// New validates a finite, opt-in execution contract; it never searches PATH.
func New(config Config) (*Runner, error) {
	if !filepath.IsAbs(config.Executable) || config.Timeout <= 0 || config.MaxRequestBytes <= 0 ||
		config.MaxRequestBytes == int64(^uint64(0)>>1) || config.MaxOutputBytes <= 0 {
		return nil, fmt.Errorf("worker requires an absolute executable and positive finite budgets")
	}
	r := &Runner{config: config, args: []string{"--protocol=1", "--max-input=" + strconv.FormatInt(config.MaxRequestBytes, 10)}, env: []string{}}
	if runtime.GOOS == "windows" {
		r.env = append(r.env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return r, nil
}

// Run covers startup, stdin, parsing, execution and transport with an external
// deadline. It kills the direct child on cancellation/overflow and joins I/O
// after exactly one Wait. OS-uninterruptible processes can still delay Wait.
func (r *Runner) Run(ctx context.Context, request Request) (result Result, err error) {
	if ctx == nil {
		return result, fmt.Errorf("worker requires a context")
	}
	ctx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = validateRequest(request, r.config.MaxRequestBytes); err != nil {
		return result, err
	}
	payload, err := json.Marshal(wireRequest{Protocol: 1, Request: request})
	if err != nil {
		return result, err
	}
	if int64(len(payload)) > r.config.MaxRequestBytes {
		return result, ErrInputLimit
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	dir, err := os.MkdirTemp("", "starbox-worker-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	files := make([]*os.File, 0, 6)
	closeFiles := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	defer closeFiles()
	for i := 0; i < 3; i++ {
		read, write, e := os.Pipe()
		if e != nil {
			return result, e
		}
		files = append(files, read, write)
	}
	inR, inW, outR, outW, errR, errW := files[0], files[1], files[2], files[3], files[4], files[5]
	// The executable comes from trusted host configuration; argv contains only
	// fixed protocol options. Request data travels on stdin, never through a shell.
	cmd := exec.Command(r.config.Executable, r.args...)
	cmd.Dir = dir
	cmd.Env = append([]string{}, r.env...)
	cmd.Stdin = inR
	cmd.Stdout = outW
	cmd.Stderr = errW
	if err = cmd.Start(); err != nil {
		return result, err
	}
	result.PID = cmd.Process.Pid
	_ = inR.Close()
	_ = outW.Close()
	_ = errW.Close()
	captured := &capture{limit: r.config.MaxOutputBytes, exceeded: make(chan struct{})}
	var group sync.WaitGroup
	pipeErrors := make(chan error, 3)
	group.Add(3)
	copyStream := func(reader *os.File, dst *bytes.Buffer) {
		defer group.Done()
		_, e := io.Copy(streamCapture{captured, dst}, reader)
		pipeErrors <- e
	}
	go copyStream(outR, &captured.stdout)
	go copyStream(errR, &captured.stderr)
	go func() { defer group.Done(); _, e := inW.Write(payload); _ = inW.Close(); pipeErrors <- e }()
	ioDone := make(chan struct{})
	go func() { group.Wait(); close(ioDone) }()
	// Manual pipes avoid Cmd.Wait closing readers before final bytes are consumed.
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var waitErr, cause error
	select {
	case waitErr = <-waited:
	case <-ctx.Done():
		cause = ctx.Err()
	case <-captured.exceeded:
		cause = ErrOutputLimit
	}
	if cause != nil {
		_ = cmd.Process.Kill()
		waitErr = <-waited
	}
	result.Reaped = cmd.ProcessState != nil
	if result.Reaped {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	_ = inW.Close()
	cause = finishIO(ctx, ioDone, closeFiles, cause)
	closeFiles()
	result.PipesClosed = true
	result.Stdout = captured.stdout.Bytes()
	result.Stderr = captured.stderr.Bytes()
	result.OutputTruncated = captured.truncated
	if cause == nil && captured.truncated {
		cause = ErrOutputLimit
	}
	if cause != nil {
		return result, cause
	}
	if waitErr != nil {
		return result, fmt.Errorf("worker exited: %w", waitErr)
	}
	response := wireResponse{}
	if e := decodeOne(result.Stdout, &response); e != nil {
		return result, e
	}
	if response.Protocol != 1 || (response.OK && (response.Error != "" || response.Kind != "")) ||
		(!response.OK && (response.Error == "" || len(response.Result) > 0)) {
		return result, ErrProtocol
	}
	for i := 0; i < 3; i++ {
		if e := <-pipeErrors; e != nil {
			return result, fmt.Errorf("worker pipe: %w", e)
		}
	}
	if !response.OK {
		return result, &RemoteError{Kind: response.Kind, Message: response.Error}
	}
	result.Payload = response.Result
	return result, nil
}

// Bound parent cleanup if an uncontained descendant holds a pipe open. Closing
// the endpoints must wake the I/O goroutines; done joins them before return.
func finishIO(ctx context.Context, done <-chan struct{}, closeFiles func(), cause error) error {
	grace := time.NewTimer(200 * time.Millisecond)
	defer grace.Stop()
	select {
	case <-done:
	case <-ctx.Done():
		if cause == nil {
			cause = ctx.Err()
		}
		closeFiles()
		<-done
	case <-grace.C:
		if cause == nil {
			cause = fmt.Errorf("%w: pipes did not close after exit", ErrProtocol)
		}
		closeFiles()
		<-done
	}
	return cause
}

type capture struct {
	mu             sync.Mutex
	stdout, stderr bytes.Buffer
	limit, kept    int64
	exceeded       chan struct{}
	truncated      bool
}
type streamCapture struct {
	c   *capture
	dst *bytes.Buffer
}

func (s streamCapture) Write(p []byte) (int, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	keep := int64(len(p))
	if remain := s.c.limit - s.c.kept; keep > remain {
		keep = remain
		if !s.c.truncated {
			s.c.truncated = true
			close(s.c.exceeded)
		}
	}
	_, _ = s.dst.Write(p[:int(keep)])
	s.c.kept += keep
	return len(p), nil
}
