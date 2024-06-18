package starbox_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/1set/starbox"
	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv"
	"github.com/psanford/memfs"
	"go.starlark.net/starlark"
)

var (
	hereDoc   = starbox.HereDocf
	noopPrint = func(thread *starlark.Thread, msg string) {
		return
	}
)

// TestProbe is a playground for exploring the external packages.
func TestProbe(t *testing.T) {
	x := starlet.GetAllBuiltinModuleNames()
	xj, _ := json.Marshal(x)
	t.Log(string(xj))
}

// TestNew tests the following:
// 1. Create a new Starbox instance.
// 2. Check the Stringer output.
// 3. Check the underlying starlet.Machine instance.
func TestNew(t *testing.T) {
	b := starbox.New("test")
	n := `🥡Box{name:test,run:0}`
	if a := b.String(); a != n {
		t.Errorf("expect %s, got %s", n, a)
	}
	m := b.GetMachine()
	if m == nil {
		t.Error("expect not nil, got nil")
	}
}

// TestSetStructTag tests the following:
// 1. Create a new Starbox instance.
// 2. Set the struct tag.
// 3. Run a script that uses the custom struct tag.
// 4. Check the output.
func TestSetStructTag(t *testing.T) {
	type testStruct struct {
		Nick1 string `json:"nick"`
		Nick2 string `starlark:"nick"`
	}
	s := testStruct{
		Nick1: "Kai",
		Nick2: "Kalani",
	}
	tests := []struct {
		tag      string
		expected string
	}{
		{"json", "Kai"},
		{"starlark", "Kalani"},
		{"", "Kalani"},
	}
	for i, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			b := starbox.New(fmt.Sprintf("test_%d", i))
			if tt.tag != "" {
				b.SetStructTag(tt.tag)
			}
			b.AddKeyValue("data", s)
			out, err := b.Run(hereDoc(`
				s = data.nick
				print(data)
			`))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
				return
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
				return
			}
			if es := tt.expected; out["s"] != es {
				t.Errorf("expect %q, got %v", es, out["s"])
			}
		})
	}

}

// TestSetPrintFunc tests the following:
// 1. Create a new Starbox instance.
// 2. Set the print function to output to a buffer.
// 3. Run a script that uses the print function.
// 4. Check the output.
func TestSetPrintFunc(t *testing.T) {
	var sb strings.Builder
	b := starbox.New("test")
	b.SetPrintFunc(func(thread *starlark.Thread, msg string) {
		sb.WriteString(msg)
	})
	out, err := b.Run(hereDoc(`
		print('Aloha!')
		print('Mahalo!')
	`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 0 {
		t.Errorf("expect 0, got %d", len(out))
	}
	actual := sb.String()
	expected := "Aloha!Mahalo!"
	if actual != expected {
		t.Errorf("expect %q, got %v", expected, actual)
	}
}

// TestSetFS tests the following:
// 1. Create a virtual filesystem.
// 2. Create a new Starbox instance.
// 3. Set the virtual filesystem, and add a module script.
// 4. Run a script that uses the virtual filesystem.
// 5. Check the output -- the virtual filesystem should override the module script.
// 6. Rerun the script with the same virtual filesystem.
// 7. Check the output -- the virtual filesystem should persist.
func TestSetFS(t *testing.T) {
	// create a virtual filesystem
	s1 := hereDoc(`
		a = 10
		b = 20
	`)
	s2 := hereDoc(`
		a = 100
		b = 200
	`)
	mn := `test.star`
	fs := memfs.New()
	fs.WriteFile(mn, []byte(s1), 0644)

	// create a new Starbox instance
	b := starbox.New("test")
	b.SetFS(fs)
	b.AddModuleScript(mn, s2)

	// run a script that uses the virtual filesystem
	out, err := b.Run(hereDoc(`
		load("test.star", "a", "b")
		c = a + b
		print(__modules__)
		m = len(__modules__)
	`))
	if err != nil {
		t.Error(err)
		return
	}
	if out == nil {
		t.Error("expect not nil, got nil")
		return
	}
	if len(out) != 2 {
		t.Errorf("expect 2, got %d", len(out))
		return
	}
	if es := int64(30); out["c"] != es {
		t.Errorf("expect %d, got %v", es, out["c"])
		return
	}
	if es := int64(0); out["m"] != es {
		t.Errorf("expect %d, got %v", es, out["m"])
		return
	}

	// rerun the script with the same virtual filesystem
	out, err = b.Run(hereDoc(`
		load("test.star", "a", "b")
		d = a * b
	`))
	if err != nil {
		t.Error(err)
		return
	}
	if out == nil {
		t.Error("expect not nil, got nil")
		return
	}
	if len(out) != 1 {
		t.Errorf("expect 1, got %d", len(out))
		return
	}
	if es := int64(200); out["d"] != es {
		t.Errorf("expect %d, got %v", es, out["d"])
		return
	}
}

// TestSetModuleSet tests the following:
// 1. Create a new Starbox instance.
// 2. Set the module set.
// 3. Run a script that uses the module set.
// 4. Check the output.
func TestSetModuleSet(t *testing.T) {
	tests := []struct {
		setName starbox.ModuleSetName
		wantErr bool
		hasMod  []string
		nonMod  []string
	}{
		{
			setName: starbox.ModuleSetName("unknown"),
			wantErr: true,
		},
		{
			// empty module set for default
			nonMod: []string{"base64", "json", "go_idiomatic"},
		},
		{
			setName: starbox.EmptyModuleSet,
			nonMod:  []string{"base64", "json", "go_idiomatic"},
		},
		{
			setName: starbox.SafeModuleSet,
			hasMod:  []string{"base64", "json", "sleep", "exit"},
			nonMod:  []string{"http", "runtime", "go_idiomatic", "file", "path"},
		},
		{
			setName: starbox.NetworkModuleSet,
			hasMod:  []string{"base64", "json", "sleep", "exit", "http"},
			nonMod:  []string{"runtime", "go_idiomatic", "file", "path"},
		},
		{
			setName: starbox.FullModuleSet,
			hasMod:  []string{"sleep", "exit", "atom", "base64", "csv", "file", "hashlib", "http", "json", "log", "math", "path", "random", "re", "runtime", "string", "time"},
			nonMod:  []string{"go_idiomatic"},
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			getBox := func() *starbox.Starbox {
				name := fmt.Sprintf("test_%d", i)
				b := starbox.New(name)
				b.SetModuleSet(tt.setName)
				b.SetPrintFunc(noopPrint)
				return b
			}

			if tt.wantErr {
				b := getBox()
				_, err := b.Run(hereDoc(`a = 1`))
				if err == nil {
					t.Error("expect error, got nil")
				}
				return
			}

			// check for existing modules
			for _, m := range tt.hasMod {
				b := getBox()
				res, err := b.Run(hereDoc(fmt.Sprintf(`print(type(%s)); m = __modules__`, m)))
				if err != nil {
					t.Errorf("expect nil for existing module %q, got %v", m, err)
					return
				}
				t.Logf("{%s} modules: %v", tt.setName, res["m"])
			}

			// check for non-existing modules
			for _, m := range tt.nonMod {
				b := getBox()
				_, err := b.Run(hereDoc(fmt.Sprintf(`print(type(%s))`, m)))
				if err == nil {
					t.Errorf("expect error for non-existing module %q, got nil", m)
					return
				}
			}
		})
	}
}

// TestAddKeyValue tests the following:
// 1. Create a new Starbox instance.
// 2. Add a key-value pair.
// 3. Run a script that uses the key-value pair.
// 4. Check the output to see if the key-value pair is present.
func TestAddKeyValue(t *testing.T) {
	b := starbox.New("test")
	b.AddKeyValue("a", 10)
	b.AddKeyValue("b", 20)
	out, err := b.Run(hereDoc(`c = a + b`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 1 {
		t.Errorf("expect 1, got %d", len(out))
	}
	if es := int64(30); out["c"] != es {
		t.Errorf("expect %d, got %v", es, out["c"])
	}
}

// TestAddKeyStarlarkValue tests the following:
// 1. Create a new Starbox instance.
// 2. Add a key-Starlark value pair.
// 3. Run a script that uses the key-Starlark value pair.
// 4. Check the output to see if the key-Starlark value pair is present.
func TestAddKeyStarlarkValue(t *testing.T) {
	b := starbox.New("test")
	b.AddKeyStarlarkValue("a", starlark.MakeInt(11))
	b.AddKeyStarlarkValue("b", starlark.MakeInt(22))
	out, err := b.Run(hereDoc(`c = a + b`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 1 {
		t.Errorf("expect 1, got %d", len(out))
	}
	if es := int64(33); out["c"] != es {
		t.Errorf("expect %d, got %v", es, out["c"])
	}
}

// TestAddKeyValues tests the following:
// 1. Create a new Starbox instance.
// 2. Add key-value pairs.
// 3. Run a script that uses the key-value pairs.
// 4. Check the output to see if the key-value pairs are present.
func TestAddKeyValues(t *testing.T) {
	b := starbox.New("test")
	b.AddKeyValues(starlet.StringAnyMap{
		"a": 10,
		"b": 20,
	})
	b.AddKeyValues(starlet.StringAnyMap{
		"c": 30,
	})
	out, err := b.Run(hereDoc(`d = a + b + c`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 1 {
		t.Errorf("expect 1, got %d", len(out))
	}
	if es := int64(60); out["d"] != es {
		t.Errorf("expect %d, got %v", es, out["d"])
	}
}

// TestAddStarlarkValues tests the following:
// 1. Create a new Starbox instance.
// 2. Add key-value pairs.
// 3. Run a script that uses the key-value pairs.
// 4. Check the output to see if the key-value pairs are present.
func TestAddStarlarkValues(t *testing.T) {
	b := starbox.New("test")
	b.AddStarlarkValues(starlark.StringDict{
		"a": starlark.MakeInt(11),
		"b": starlark.MakeInt(22),
	})
	b.AddStarlarkValues(starlark.StringDict{
		"c": starlark.Float(33),
	})
	out, err := b.Run(hereDoc(`d = a + b + c`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 1 {
		t.Errorf("expect 1, got %d", len(out))
	}
	if es := float64(66); out["d"] != es {
		t.Errorf("expect %f, got %v", es, out["d"])
	}
}

// TestAddBuiltin tests the following:
// 1. Create a new Starbox instance.
// 2. Add a builtin function.
// 3. Run a script that uses the builtin function.
// 4. Check the output to see if the builtin function works.
func TestAddBuiltin(t *testing.T) {
	b := starbox.New("test")
	b.AddBuiltin("shift", func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var a, b int64
		if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
			return nil, err
		}
		return starlark.MakeInt64(a << b).Add(starlark.MakeInt(3)), nil
	})
	out, err := b.Run(hereDoc(`
		c = shift(a=10, b=4)
	`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 1 {
		t.Errorf("expect 1, got %d", len(out))
	}
	if es := int64(163); out["c"] != es {
		t.Errorf("expect %d, got %v", es, out["c"])
	}
}

// TestAddNamedModules tests the following:
// 1. Create a new Starbox instance.
// 2. Add named modules.
// 3. Run a script that uses function from the named modules.
// 4. Check the output to see if the named modules are present.
func TestAddNamedModules(t *testing.T) {
	b := starbox.New("test")
	b.AddNamedModules("runtime")
	b.AddNamedModules("base64")
	b.AddNamedModules("runtime")
	out, err := b.Run(hereDoc(`
		s = base64.encode('Aloha!')
		t = type(runtime.pid)
		m = __modules__
	`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 3 {
		t.Errorf("expect 3, got %d", len(out))
	}
	if es := `QWxvaGEh`; out["s"] != es {
		t.Errorf("expect %q, got %v", es, out["s"])
	}
	if es := `int`; out["t"] != es {
		t.Errorf("expect %q, got %v", es, out["t"])
	}
	if es := []interface{}{"base64", "runtime"}; !reflect.DeepEqual(out["m"].([]interface{}), es) {
		t.Errorf("expect %v, got %v", es, out["m"])
	}
}

// TestAddModuleLoader tests the following:
// 1. Create a new Starbox instance.
// 2. Add a module loader.
// 3. Run a script that uses function from the module loader.
// 4. Check the output to see if the module loader works.
func TestAddModuleLoader(t *testing.T) {
	b := starbox.New("test")
	b.AddModuleLoader("mine", func() (starlark.StringDict, error) {
		return starlark.StringDict{
			"shift": starlark.NewBuiltin("shift", func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				var a, b int64
				if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
					return nil, err
				}
				return starlark.MakeInt64(a << b).Add(starlark.MakeInt(5)), nil
			}),
			"num": starlark.MakeInt(100),
		}, nil
	})
	b.AddModuleLoader("more", dataconv.WrapModuleData("less", starlark.StringDict{
		"num": starlark.MakeInt(200),
		"plus": starlark.NewBuiltin("plus", func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var a, b int64
			if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
				return nil, err
			}
			return starlark.MakeInt64(a + b), nil
		}),
	}))
	tests := []struct {
		script string
		want   int64
	}{
		{`print(__modules__); c = len(__modules__)`, 2},
		{`c = shift(a=10, b=4) + num`, 265},
		{`load("mine", "shift", "num"); c = shift(a=10, b=5) * num`, 32500},
		{`c = less.plus(a=10, b=4) + less.num + num`, 314},
		{`load("more", "less"); c = less.plus(a=10, b=5) * less.num`, 3000},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b.Reset()
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}

// TestAddModuleData tests the following:
// 1. Create a new Starbox instance.
// 2. Add module data.
// 3. Run a script that uses function from the module data.
// 4. Check the output to see if the module data works.
func TestAddModuleData(t *testing.T) {
	b := starbox.New("test")
	b.AddModuleData("data", starlark.StringDict{
		"a": starlark.MakeInt(10),
		"b": starlark.MakeInt(20),
		"c": starlark.MakeInt(300),
	})
	tests := []struct {
		script string
		want   int64
	}{
		{`print(__modules__); c = len(__modules__)`, 1},
		{`c = data.a + data.b`, 30},
		{`load("data", "a", "b"); c = a * b`, 200},
		{`load("data", "a", "b"); c = data.c * (a+b)`, 9000},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b.Reset()
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}

// TestAddModuleFunctions tests the following:
// 1. Create a new Starbox instance.
// 2. Add module functions.
// 3. Run a script that uses function from the module functions.
// 4. Check the output to see if the module functions work.
func TestAddModuleFunctions(t *testing.T) {
	b := starbox.New("test")
	b.AddModuleFunctions("data", starbox.FuncMap{
		"shift": func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var a, b int64
			if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
				return nil, err
			}
			return starlark.MakeInt64(a << b).Add(starlark.MakeInt(7)), nil
		},
	})
	tests := []struct {
		script string
		want   int64
	}{
		{`print(__modules__); c = len(__modules__)`, 1},
		{`c = data.shift(a=10, b=4) + 100`, 267},
		{`load("data", "shift"); c = shift(a=10, b=5) * 10`, 3270},
		{`c = int(str(data.shift) == '<built-in function data.shift>')`, 1},
		{`load("data", "shift"); c = int(str(shift) == '<built-in function data.shift>')`, 1},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b.Reset()
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}

// TestAddStructData tests the following:
// 1. Create a new Starbox instance.
// 2. Add struct data.
// 3. Run a script that uses function from the struct data.
// 4. Check the output to see if the struct data works.
func TestAddStructData(t *testing.T) {
	type dataStruct struct {
		A int64
		B int64
		C int64
	}
	b := starbox.New("test")
	b.AddStructData("data", starlark.StringDict{
		"A": starlark.MakeInt(10),
		"B": starlark.MakeInt(20),
		"C": starlark.MakeInt(300),
	})
	tests := []struct {
		script string
		want   int64
	}{
		{`print(__modules__); c = len(__modules__)`, 1},
		{`c = data.A + data.B`, 30},
		{`c = data.A * data.B`, 200},
		{`c = data.C * (data.A + data.B)`, 9000},
		{`load("data", 'A', 'B'); c = A * B`, 200},
		{`load("data", 'A', 'B', 'C'); c = C * (A + B)`, 9000},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b.Reset()
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}

// TestAddStructFunctions tests the following:
// 1. Create a new Starbox instance.
// 2. Add struct functions.
// 3. Run a script that uses function from the struct functions.
// 4. Check the output to see if the struct functions work.
func TestAddStructFunctions(t *testing.T) {
	b := starbox.New("test")
	b.AddStructFunctions("data", starbox.FuncMap{
		"shift": func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var a, b int64
			if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
				return nil, err
			}
			return starlark.MakeInt64(a << b).Add(starlark.MakeInt(7)), nil
		},
	})
	tests := []struct {
		script string
		want   int64
	}{
		{`print(__modules__); c = len(__modules__)`, 1},
		{`c = data.shift(a=10, b=4) + 100`, 267},
		{`load("data", "shift"); c = shift(a=10, b=5) * 10`, 3270},
		{`c = int(str(data.shift) == '<built-in function data.shift>')`, 1},
		{`load("data", "shift"); c = int(str(shift) == '<built-in function data.shift>')`, 1},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b.Reset()
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}

// TestAddModuleScript tests the following:
// 1. Create a new Starbox instance.
// 2. Add module script.
// 3. Run a script that uses function from the module script.
// 4. Check the output to see if the module script works.
func TestAddModuleScript(t *testing.T) {
	b := starbox.New("test")
	b.AddModuleScript("data", hereDoc(`
		a = 10
		b = 20
		c = 300
		def shift(a, b):
			return (a << b) + 10
	`))
	tests := []struct {
		script string
		want   int64
	}{
		{`print(__modules__); c = len(__modules__)`, 1},
		{`load("data.star", "a", "b"); c = a * b`, 200},
		{`load("data", "shift"); c = shift(2, 10)`, 2058},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b.Reset()
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				t.Error(err)
				return
			}
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}

// TestAddNamedModuleAndModuleScript tests the following:
// 1. Create a new Starbox instance.
// 2. Add named modules and module script.
// 3. Run a script that uses function from the named modules and module script.
// 4. Check the output to see if the named modules and module script conflict.
func TestAddNamedModuleAndModuleScript(t *testing.T) {
	b := starbox.New("test")
	b.AddNamedModules("base64")
	b.AddNamedModules("csv")
	b.AddNamedModules("runtime")
	b.AddModuleScript("runtime", hereDoc(`
		pid = "ABC"
	`))
	out, err := b.Run(hereDoc(`
		v1 = runtime.pid	# builtin
		print("v1[b]", type(v1), v1)

		load("runtime", p2="pid")	# builtin
		v2 = p2
		print("v2[b]", type(v2), v2)

		load("runtime.star", p3="pid") # script
		v3 = p3
		print("v3[s]", type(v3), v3)

		s = " ".join([type(v1), type(v2), type(v3)])
		m = __modules__
		print(__modules__)
	`))
	if err != nil {
		t.Error(err)
	}
	if out == nil {
		t.Error("expect not nil, got nil")
	}
	if len(out) != 5 {
		t.Errorf("expect 5, got %d", len(out))
	}
	if es := `int int string`; out["s"] != es {
		t.Errorf("expect %q, got %v", es, out["s"])
	}
	if es := []interface{}{"base64", "csv", "runtime", "runtime.star"}; !reflect.DeepEqual(out["m"].([]interface{}), es) {
		t.Errorf("expect %v, got %v", es, out["m"])
	}
	if em := []string{"base64", "csv", "runtime", "runtime.star"}; !reflect.DeepEqual(em, b.GetModuleNames()) {
		t.Errorf("expect %v, got %v", em, b.GetModuleNames())
		return
	}
}

// TestSetScriptCache tests the following:
// 1. Create a new Starbox instance, and cache is enabled by default.
// 2. Local script from the filesystem.
// 3. Run a script that uses the local script.
// 4. Modify the local script.
// 5. Run the script again, check if the output is the same.
// 6. Disable the cache.
// 7. Run the script again, check if the output is different.
// 8. Enable the cache with custom provider.
// 9. Run the script again, check if the output is the same.
func TestSetScriptCache(t *testing.T) {
	// scripts for virtual filesystem
	s1 := hereDoc(`
		a = 10
		b = 20
		c = a + b
	`)
	s2 := hereDoc(`
		a = 100
		b = 200
		c = a + b
	`)
	mn := `test.star`

	// run a script that uses the local script
	testRun := func(b *starbox.Starbox, cas int, es int64) {
		out, err := b.RunFile(mn)
		if err != nil {
			t.Errorf("[%d] fail to run: %v", cas, err)
			return
		}
		if out["c"] != es {
			t.Errorf("[%d] expect %d, got %v", cas, es, out["c"])
			return
		}
	}

	{
		// create a new Starbox instance with the default cache
		b := starbox.New("test")
		fs := memfs.New()
		b.SetFS(fs)

		// run the script with the default cache
		fs.WriteFile(mn, []byte(s1), 0644)
		testRun(b, 1, 30)

		// modify file content, and run the script again -- dirty cache
		fs.WriteFile(mn, []byte(s2), 0644)
		testRun(b, 2, 30)
	}

	{
		// create a new Starbox instance and then disable cache
		b := starbox.New("test")
		fs := memfs.New()
		b.SetFS(fs)
		b.SetScriptCache(nil) // disable cache

		// run the script without cache
		fs.WriteFile(mn, []byte(s1), 0644)
		testRun(b, 3, 30)

		// modify file content, and run the script again -- no cache
		fs.WriteFile(mn, []byte(s2), 0644)
		testRun(b, 4, 300)
	}

	{
		// create a new Starbox instance
		b := starbox.New("test")
		fs := memfs.New()
		b.SetFS(fs)
		b.SetScriptCache(starlet.NewMemoryCache()) // enable cache with custom provider

		// run the script with the custom cache
		fs.WriteFile(mn, []byte(s1), 0644)
		testRun(b, 5, 30)

		// modify file content, and run the script again -- cache
		fs.WriteFile(mn, []byte(s2), 0644)
		testRun(b, 6, 30)
	}
}

// TestDynamicModuleLoader tests the following:
// 1. Create a new Starbox instance.
// 2. Add a module loader.
// 3. Set dynamic module loader.
// 4. Run a script that uses function from the module loader.
// 5. Check the output to see if the module loader works.
func TestDynamicModuleLoader(t *testing.T) {
	buildBox := func(b *starbox.Starbox) {
		b.AddModuleLoader("math", func() (starlark.StringDict, error) {
			return starlark.StringDict{
				"num": starlark.MakeInt(100),
				"shift": starlark.NewBuiltin("shift", func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
					var a, b int64
					if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
						return nil, err
					}
					return starlark.MakeInt64(a << b).Add(starlark.MakeInt(5)), nil
				}),
			}, nil
		})
		b.AddModuleLoader("more", dataconv.WrapModuleData("less", starlark.StringDict{
			"num": starlark.MakeInt(200),
			"plus": starlark.NewBuiltin("plus", func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				var a, b int64
				if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
					return nil, err
				}
				return starlark.MakeInt64(a + b + 1000), nil
			}),
		}))
		b.SetDynamicModuleLoader(func(s string) (starlet.ModuleLoader, error) {
			if s == "aloha" || s == "atom" {
				return dataconv.WrapModuleData("minus", starlark.StringDict{
					"num": starlark.MakeInt(500),
					"minus": starlark.NewBuiltin("minus", func(thread *starlark.Thread, bt *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
						var a, b int64
						if err := starlark.UnpackArgs(bt.Name(), args, kwargs, "a", &a, "b", &b); err != nil {
							return nil, err
						}
						return starlark.MakeInt64(a + b - 2000), nil
					}),
				}), nil
			} else if s == "mistake" {
				return nil, errors.New("a mistake")
			} else if s == "empty" {
				return nil, nil
			}
			return nil, errors.New("kaumaha")
		})
	}

	tests := []struct {
		builder func(*starbox.Starbox)
		script  string
		want    int64
		wantErr bool
	}{
		{
			// 0. add named module from starlet
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("atom")
			},
			script: `print(__modules__); c = int("|".join(__modules__) == 'atom|math|more')`,
			want:   1,
		},
		{
			// 1.duplicate named module from dynamic, no affect -- check load starlet
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("atom")
			},
			script: `print(dir(atom)); c = int("|".join(dir(atom)) == 'new_float|new_int|new_string')`,
			want:   1,
		},
		{
			// 2. conflict named module from custom, override starlet -- check load starlet
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("math")
			},
			script: `print(dir(math)); c = int(len(dir(math)) > 2)`,
			want:   1,
		},
		{
			// 3. duplicate named module from starlet or custom, no affect
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("math", "more")
			},
			script: `print(__modules__); c = int("|".join(__modules__) == 'math|more')`,
			want:   1,
		},
		{
			// 4. duplicate named module from starlet or custom, no affect
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("more")
			},
			script: `c = less.num`,
			want:   200,
		},
		{
			// 5. add named module from dynamic
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("aloha")
			},
			script: `c = minus.num`,
			want:   500,
		},
		{
			// 6. add named module from dynamic, and also from starlet
			builder: func(b *starbox.Starbox) {
				b.AddNamedModules("aloha", "atom")
			},
			script: `print(__modules__); c = int("|".join(__modules__) == 'aloha|atom|math|more')`,
			want:   1,
		},
		{
			// 7. use alias
			builder: func(b *starbox.Starbox) {
				b.AddModulesByName("aloha", "atom")
			},
			script: `print(__modules__); c = int("|".join(__modules__) == 'aloha|atom|math|more')`,
			want:   1,
		},
		{
			// 8. missing module
			builder: func(b *starbox.Starbox) {
				b.AddModulesByName("aloha", "mahalo")
			},
			script:  `print(__modules__)`,
			wantErr: true,
		},
		{
			// 9. another missing module
			builder: func(b *starbox.Starbox) {
				b.AddModulesByName("aloha", "empty")
			},
			script:  `print(__modules__)`,
			wantErr: true,
		},
		{
			// 10. load dynamic module with error
			builder: func(b *starbox.Starbox) {
				b.AddModulesByName("mistake")
			},
			script:  `print(__modules__)`,
			wantErr: true,
		},
		{
			// 11. disable dynamic module loader
			builder: func(b *starbox.Starbox) {
				b.AddModulesByName("aloha", "atom")
				b.SetDynamicModuleLoader(nil)
			},
			script:  `print(__modules__)`,
			wantErr: true,
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			// build box
			b := starbox.New(fmt.Sprintf("test_%d", i))
			buildBox(b)
			if build := tt.builder; build != nil {
				build(b)
			}

			// run script
			out, err := b.Run(hereDoc(tt.script))
			if err != nil {
				if !tt.wantErr {
					t.Errorf("expect nil, got error: %v", err)
				}
				return
			} else if tt.wantErr {
				t.Error("expect error, got nil")
				return
			}

			// check result
			if out == nil {
				t.Error("expect not nil, got nil")
			}
			if len(out) != 1 {
				t.Errorf("expect 1, got %d", len(out))
			}
			if es := tt.want; out["c"] != es {
				t.Errorf("expect %d, got %v", es, out["c"])
			}
		})
	}
}
