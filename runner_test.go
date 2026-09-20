package starbox_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/1set/starbox"
	"github.com/1set/starlet"
	"github.com/psanford/memfs"
	"go.starlark.net/starlark"
)

func TestRunnerConfig_Empty(t *testing.T) {
	cfg := starbox.NewRunConfig()
	t.Logf("config: %v", cfg)

	if _, e := cfg.Execute(); e == nil || e.Error() != "no starbox instance" {
		t.Errorf("got unexpected error: %v", e)
		return
	}

	res, err := cfg.Starbox(starbox.New("aloha")).Execute()
	if err == nil || err.Error() != "starlet: run: no script to execute" {
		t.Errorf("got unexpected error: %v", err)
		return
	}
	if len(res) > 0 {
		t.Errorf("expect empty, got %v", res)
		return
	}
}

func TestRunnerConfig_Full(t *testing.T) {
	box := starbox.New("aloha")
	box.SetModuleSet(starbox.SafeModuleSet)
	cfg := starbox.NewRunConfig().
		FileName("mine.star").
		Script("print('Hello, {}!'.format(word)); x = word.upper(); print(__modules__)").
		Context(context.TODO()).
		KeyValue("word", "World").
		Timeout(5*time.Second).
		Inspect(false).
		InspectCond(func(_ starlet.StringAnyMap, e error) bool { return e != nil }).
		KeyValue("word", "Star")
	cfg2 := cfg.Starbox(box)

	t.Logf("config1: %v", cfg)
	t.Logf("config2: %v", cfg2)

	_, e1 := cfg.Execute()
	if e1 == nil {
		t.Error("expect error, got nil")
		return
	}
	t.Logf("error1: %v", e1)

	res, e2 := cfg2.Execute()
	if e2 != nil {
		t.Errorf("expect nil, got %v", e2)
		return
	}
	if res["x"].(string) != "STAR" {
		t.Errorf("expect x=STAR, got %v", res["x"])
		return
	}
	if em := []string{"atom", "base64", "csv", "go_idiomatic", "hashlib", "json", "math", "random", "re", "regex", "serial", "stats", "string", "struct", "time"}; !reflect.DeepEqual(em, box.GetModuleNames()) {
		t.Errorf("expect %v, got %v", em, box.GetModuleNames())
		return
	}
	t.Logf("result: %v", res)
	t.Logf("box: %v", box)
}

func TestRunnerConfig_Reuse(t *testing.T) {
	cfg := starbox.New("aloha").
		CreateRunConfig().
		FileName("mine.star").
		Script("print('Hello, {}!'.format(word)); x = word.upper()").
		Timeout(-1*time.Nanosecond).
		Inspect(false).
		KeyValue("word", "World")
	res, err := cfg.Execute()
	if err != nil {
		t.Errorf("expect nil, got %v", err)
		return
	}
	if res["x"].(string) != "WORLD" {
		t.Errorf("expect x=WORLD, got %v", res["x"])
		return
	}

	// reuse the same config
	box2 := starbox.New("hello")
	res2, err2 := cfg.Starbox(box2).Execute()
	if err2 != nil {
		t.Errorf("expect nil, got %v", err2)
		return
	}
	if res2["x"].(string) != "WORLD" {
		t.Errorf("expect x=WORLD, got %v", res2["x"])
		return
	}

	// reuse the box
	res3, err3 := cfg.Starbox(box2).Execute()
	if err3 != nil {
		t.Errorf("expect nil, got %v", err3)
		return
	}
	if res3["x"].(string) != "WORLD" {
		t.Errorf("expect x=WORLD, got %v", res3["x"])
		return
	}
	t.Logf("box2: %v", box2)
}

func TestRunnerConfig_OutputLimit(t *testing.T) {
	// Execute() must enforce SetMaxOutputEntries just like Run() does (P1-1).
	b := starbox.New("runner-limit")
	b.SetPrintFunc(noopPrint)
	b.SetMaxOutputEntries(1)

	_, err := b.CreateRunConfig().Script("a = 1\nb = 2").Execute()
	var oe starbox.OutputLimitExceededError
	if !errors.As(err, &oe) {
		t.Errorf("Execute should enforce the output limit; got %T: %v", err, err)
	}
}

func TestRunnerConfig_KeyValues(t *testing.T) {
	cfg := starbox.New("aloha").CreateRunConfig().
		KeyValueMap(starlet.StringAnyMap{"a": 1}).
		KeyValue("a", 10).
		KeyValue("b", 20).
		KeyValueMap(starlet.StringAnyMap{"a": 100, "c": 50}).
		KeyValue("c", 1000).
		KeyValueMap(starlet.StringAnyMap{"d": 10000}).
		Script("r = a + b + c + d")
	res, err := cfg.Execute()
	if err != nil {
		t.Errorf("expect nil, got %v", err)
		return
	}
	if res["r"].(int64) != 11120 {
		t.Errorf("expect r=11120, got %v", res["r"])
		return
	}
}

func TestRunnerConfig_Clone(t *testing.T) {
	cfg := starbox.New("aloha").CreateRunConfig().
		KeyValue("a", 10).
		KeyValue("b", 20).
		KeyValue("c", 30).
		Script("r = a + b + c")
	cfg2 := cfg.Clone()
	cfg3 := cfg2.KeyValue("a", 100).KeyValue("b", 200).KeyValue("c", 300)
	// compare pointers
	if cfg == cfg2 {
		t.Error("expect different pointers, got same")
		return
	}
	for i, c := range []*starbox.RunnerConfig{cfg, cfg2, cfg3} {
		out, err := c.Execute()
		want := []int64{60, 60, 600}[i]
		if err != nil || out["r"] != want {
			t.Errorf("config %d: result = %v, error = %v; want %d", i, out, err, want)
		}
	}
}

// Runner configuration isolation:
//   - Every fluent method preserves the original and sibling parameter bindings.
//   - Input maps are copied, including when adding to an existing configuration.
//   - Concurrent branches can execute independently on separate boxes.
func TestRunnerConfig_ParameterIsolation(t *testing.T) {
	methods := map[string]func(*starbox.RunnerConfig) *starbox.RunnerConfig{
		"Clone":       (*starbox.RunnerConfig).Clone,
		"Context":     func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.Context(context.Background()) },
		"FileName":    func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.FileName("copy.star") },
		"Inspect":     func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.Inspect(false) },
		"InspectCond": func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.InspectCond(nil) },
		"KeyValue":    func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.KeyValue("extra", 1) },
		"KeyValueMap": func(c *starbox.RunnerConfig) *starbox.RunnerConfig {
			return c.KeyValueMap(starlet.StringAnyMap{"extra": 1})
		},
		"Script":  func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.Script("r = word + suffix") },
		"Starbox": func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.Starbox(starbox.New("copy")) },
		"Timeout": func(c *starbox.RunnerConfig) *starbox.RunnerConfig { return c.Timeout(time.Minute) },
	}
	// Pin the complete fluent surface: a new method must join this contract test.
	typ := reflect.TypeOf(starbox.NewRunConfig())
	var actual, covered []string
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if m.Type.NumOut() == 1 && m.Type.Out(0) == typ {
			actual = append(actual, m.Name)
		}
	}
	for name := range methods {
		covered = append(covered, name)
	}
	slices.Sort(covered)
	if !reflect.DeepEqual(actual, covered) {
		t.Fatalf("fluent method coverage: got %v; want %v", covered, actual)
	}
	for name, derive := range methods {
		t.Run(name, func(t *testing.T) {
			base := starbox.NewRunConfig().Script("r = word + suffix").
				KeyValueMap(starlet.StringAnyMap{"word": "base", "suffix": "!"})
			derived := derive(base)
			one := derived.KeyValue("word", "one")
			many := derived.KeyValueMap(starlet.StringAnyMap{"word": "many", "suffix": "?"})
			ignored := base.Clone()
			ignored.KeyValue("word", "discarded")
			ignored.KeyValueMap(starlet.StringAnyMap{"suffix": "discarded"})
			configs := []*starbox.RunnerConfig{base, derived, one, many, ignored}
			wants := []string{"base!", "base!", "one!", "many?", "base!"}
			// Execute in both orders to catch changes leaking back from a later run.
			for _, order := range [][]int{{0, 1, 2, 3, 4}, {4, 3, 2, 1, 0}} {
				for _, i := range order {
					out, err := configs[i].Starbox(starbox.New("isolated")).Execute()
					if err != nil || out["r"] != wants[i] {
						t.Errorf("config %d: result = %v, error = %v; want %q", i, out, err, wants[i])
					}
				}
			}
		})
	}
}

func TestRunnerConfig_ParameterMapOwnership(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			base := starbox.NewRunConfig().Script("r = word")
			if existing {
				base = base.KeyValue("word", "original")
			}
			input := starlet.StringAnyMap{"word": "copied"}
			cfg := base.KeyValueMap(input).KeyValueMap(nil)
			input["word"] = "changed"
			delete(input, "word")
			out, err := cfg.Starbox(starbox.New("input-map")).Execute()
			if err != nil || out["r"] != "copied" {
				t.Fatalf("result = %v, error = %v; want copied", out, err)
			}
		})
	}
}

func TestRunnerConfig_ConcurrentBranches(t *testing.T) {
	base := starbox.NewRunConfig().Script("r = word").KeyValue("word", "base")
	for i := 0; i < 16; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for j := 0; j < 8; j++ {
				word := fmt.Sprintf("%d/%d", i, j)
				cfg := base.Clone().KeyValue("word", "first").
					KeyValueMap(starlet.StringAnyMap{"word": word}).Starbox(starbox.New("parallel"))
				out, err := cfg.Execute()
				if err != nil || out["r"] != word {
					t.Fatalf("result = %v, error = %v; want %q", out, err, word)
				}
				out, err = base.Starbox(starbox.New("template")).Execute()
				if err != nil || out["r"] != "base" {
					t.Fatalf("template result = %v, error = %v; want base", out, err)
				}
			}
		})
	}
}

func TestRunnerConfig_RunWithName(t *testing.T) {
	var sb strings.Builder
	b := starbox.New("test")
	b.SetPrintFunc(func(thread *starlark.Thread, msg string) {
		sb.WriteString(msg)
	})
	_, err := b.CreateRunConfig().FileName("one.star").Script(`print('Aloha!'`).Execute()
	if err == nil {
		t.Error("expect error, got nil")
		return
	}
	if err.Error() != "starlark: exec: one.star:1:15: got end of file, want ')'" {
		t.Errorf("expect syntax error, got %v", err)
		return
	}
}

func TestRunnerConfig_RunByName(t *testing.T) {
	// create a virtual filesystem
	mn := `exact.star`
	s1 := hereDoc(`
		a = 10
		b = 20
		print('Aloha', a+b)
	`)
	fs := memfs.New()
	fs.WriteFile(mn, []byte(s1), 0644)

	// create a new Starbox instance
	var sb strings.Builder
	b := starbox.New("test")
	b.SetFS(fs)
	b.SetPrintFunc(func(thread *starlark.Thread, msg string) {
		sb.WriteString(msg)
	})

	// run the missing script
	_, err := b.CreateRunConfig().FileName("missing.star").Execute()
	if err == nil {
		t.Error("expect error, got nil")
		return
	}

	// run the exact script
	out, err := b.CreateRunConfig().FileName(mn).Execute()
	if err != nil {
		t.Errorf("expect nil, got %v", err)
		return
	}
	if out["a"].(int64) != int64(10) || out["b"].(int64) != int64(20) {
		t.Errorf("expect a=10, b=20, got %v", out)
		return
	}
	if es := "Aloha 30"; sb.String() != es {
		t.Errorf("expect %q, got %v", es, sb.String())
		return
	}
}

func TestRunnerConfig_RunTimeout(t *testing.T) {
	b := starbox.New("test")
	b.SetModuleSet(starbox.SafeModuleSet)
	_, err := b.CreateRunConfig().Script(`sleep(1)`).Timeout(50 * time.Millisecond).Execute()
	if err == nil {
		t.Error("expect error, got nil")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Logf("expect timeout error: %v", err)
	} else {
		t.Errorf("unexpected context error, got %v", err)
	}
}

func TestRunnerConfig_RunContext(t *testing.T) {
	b := starbox.New("test")
	b.SetModuleSet(starbox.SafeModuleSet)
	ctx, cancel := context.WithCancel(context.TODO())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := b.CreateRunConfig().Script(`sleep(1)`).Context(ctx).Execute()
	if err == nil {
		t.Error("expect error, got nil")
		return
	}
	if errors.Is(err, context.Canceled) {
		t.Logf("expect cancel error: %v", err)
	} else {
		t.Errorf("unexpected context error, got %v", err)
	}
}

func TestRunnerConfig_Inspect(t *testing.T) {
	b := starbox.New("test")
	b.SetModuleSet(starbox.SafeModuleSet)
	res, err := b.CreateRunConfig().Script(`a = 100; print('Hello, World!')`).Inspect(true).Execute()
	if err != nil {
		t.Errorf("expect nil, got %v", err)
		return
	}
	if res == nil {
		t.Error("expect not nil, got nil")
		return
	}
}
