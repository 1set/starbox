package starbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/1set/starlet"
)

var (
	// ErrNoStarbox is the error for RunnerConfig.Execute() when no Starbox instance is set
	ErrNoStarbox = errors.New("no starbox instance")
)

// RunnerConfig defines the execution configuration for a Starbox instance.
type RunnerConfig struct {
	box      *Starbox
	fileName string
	script   []byte
	ctx      context.Context
	timeout  time.Duration
	condREPL InspectCondFunc
	extras   starlet.StringAnyMap
}

// String returns a string representation of the RunnerConfig.
func (c *RunnerConfig) String() string {
	var fields []string
	if c.box != nil {
		fields = append(fields, c.box.String())
	}
	if c.fileName != "" {
		fields = append(fields, fmt.Sprintf("file:%s", c.fileName))
	}
	if len(c.script) > 0 {
		fields = append(fields, fmt.Sprintf("script:%d", len(c.script)))
	}
	if c.ctx != nil && c.ctx != context.Background() {
		fields = append(fields, fmt.Sprintf("ctx:%v", c.ctx))
	}
	if c.timeout != 0 {
		fields = append(fields, fmt.Sprintf("timeout:%v", c.timeout))
	}
	if c.condREPL != nil {
		fields = append(fields, "inspect:true")
	}
	if len(c.extras) > 0 {
		fields = append(fields, fmt.Sprintf("extras:%v", c.extras))
	}
	return fmt.Sprintf("🚀Runner{%s}", strings.Join(fields, ","))
}

// NewRunConfig creates a new RunnerConfig instance.
func NewRunConfig() *RunnerConfig {
	return &RunnerConfig{}
}

// CreateRunConfig creates a new RunnerConfig instance from a given Starbox instance.
func (s *Starbox) CreateRunConfig() *RunnerConfig {
	return &RunnerConfig{box: s}
}

// FileName sets the script file name for the execution.
func (c *RunnerConfig) FileName(name string) *RunnerConfig {
	n := *c
	n.fileName = name
	return &n
}

// Script sets the script content for the execution.
func (c *RunnerConfig) Script(content string) *RunnerConfig {
	n := *c
	n.script = []byte(content)
	return &n
}

// Context sets the context for the execution.
func (c *RunnerConfig) Context(ctx context.Context) *RunnerConfig {
	n := *c
	n.ctx = ctx
	return &n
}

// Timeout sets the timeout for the execution.
func (c *RunnerConfig) Timeout(timeout time.Duration) *RunnerConfig {
	n := *c
	n.timeout = timeout
	return &n
}

// Inspect sets the inspection mode for the execution.
// It works like InspectCond with a condition function that forces the REPL mode, by adding a condition function to force the REPL mode, regardless of the output or error.
// It can be overridden by InspectCond() or Inspect().
func (c *RunnerConfig) Inspect(force bool) *RunnerConfig {
	n := *c
	n.condREPL = func(starlet.StringAnyMap, error) bool {
		return force
	}
	return &n
}

// InspectCond sets the inspection mode with a condition function for the execution.
// It can be overridden by InspectCond() or Inspect().
func (c *RunnerConfig) InspectCond(cond InspectCondFunc) *RunnerConfig {
	n := *c
	n.condREPL = cond
	return &n
}

// KeyValue sets the key-value pair for the execution.
func (c *RunnerConfig) KeyValue(key string, value interface{}) *RunnerConfig {
	n := *c
	if n.extras == nil {
		n.extras = make(starlet.StringAnyMap)
	}
	n.extras[key] = value
	return &n
}

// KeyValueMap merges the key-value pairs for the execution.
func (c *RunnerConfig) KeyValueMap(extras starlet.StringAnyMap) *RunnerConfig {
	n := *c
	if n.extras == nil {
		n.extras = make(starlet.StringAnyMap)
	}
	n.extras.Merge(extras)
	return &n
}

// Starbox sets the Starbox instance for the execution.
func (c *RunnerConfig) Starbox(b *Starbox) *RunnerConfig {
	n := *c
	n.box = b
	return &n
}

// Execute executes the box with the given configuration.
func (c *RunnerConfig) Execute() (starlet.StringAnyMap, error) {
	// config and box
	cfg := *c
	b := cfg.box
	if b == nil {
		return nil, ErrNoStarbox
	}

	// prepare variables
	if cfg.fileName == "" {
		cfg.fileName = "box.star"
	}
	if len(cfg.script) == 0 {
		cfg.script = nil
	}
	if cfg.timeout < 0 {
		cfg.timeout = 0
	}
	if cfg.ctx == nil {
		cfg.ctx = context.Background()
	}

	// handle timeout
	if cfg.timeout > 0 {
		nt, cancel := context.WithTimeout(cfg.ctx, cfg.timeout)
		defer cancel()
		cfg.ctx = nt
	}

	// if it's the first run, set the environment
	if !b.hasExec {
		if err := b.prepareEnv(); err != nil {
			return nil, err
		}
	}

	// set script things
	b.mac.SetScript(cfg.fileName, cfg.script, b.modFS)

	// finally, run the script
	b.hasExec = true
	b.execTimes++
	out, err := b.mac.RunWithContext(cfg.ctx, cfg.extras)

	// repl
	if cfg.condREPL != nil && cfg.condREPL(out, err) {
		b.mac.REPL()
	}
	return out, err
}
