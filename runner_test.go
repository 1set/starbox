package starbox_test

import (
	"context"
	"errors"
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
	cfg := starbox.NewRunConfig().
		FileName("mine.star").
		Script("print('Hello, {}!'.format(word)); x = word.upper()").
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

	res, e2 := cfg2.Execute()
	if e2 != nil {
		t.Errorf("expect nil, got %v", e2)
		return
	}
	if res["x"].(string) != "STAR" {
		t.Errorf("expect x=STAR, got %v", res["x"])
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
