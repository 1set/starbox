package starbox

import (
	"testing"

	"go.starlark.net/starlark"
)

// TestCollectiveMemory tests the following:
// 1. Create a new Starbox instance.
// 2. Created a new collective memory attached to the instance.
// 3. Run script that uses the collective memory.
// 4. Check the output to see if the collective memory works.
// 5. Create another Starbox instance.
// 6. Attach the same collective memory to the new instance.
// 7. Run another script that uses the collective memory.
// 8. Check the output to see if the collective memory persists.
func TestCollectiveMemory(t *testing.T) {
	// create a new Starbox instance: b1
	b1 := New("test1")
	mem := b1.CreateMemory("share")
	s1 := HereDoc(`
		a = 10
		b = 20
		c = a * b
		share["v"] = c
	`)
	res, err := b1.Run(s1)
	if err != nil {
		t.Errorf("b1: expect nil error, got %v", err)
		return
	}
	if res == nil {
		t.Errorf("b1: expect not nil res, got nil")
		return
	}
	// check memory:v
	av, ok, err := mem.Get(starlark.String("v"))
	if err != nil {
		t.Errorf("b1: expect nil error for memory:v, got %v", err)
		return
	}
	if ev := starlark.MakeInt(200); av != ev || !ok {
		t.Errorf("b1: expect v=%v, got %v", ev, av)
		return
	}
	t.Logf("b1: %v -- %v", res, mem)

	// create a new Starbox instance: b2
	b2 := New("test2")
	b2.AttachMemory("history", mem)
	s2 := HereDoc(`
		d = history["v"]
		e = d << 2
		history["v"] = e + 1
		history["w"] = "Aloha!"
	`)
	res, err = b2.Run(s2)
	if err != nil {
		t.Errorf("b2: expect nil error, got %v", err)
		return
	}
	if res == nil {
		t.Errorf("b2: expect not nil res, got nil")
		return
	}
	if ev := int64(200); res["d"] != ev {
		t.Errorf("b2: expect d=%v, got %v", ev, res["d"])
		return
	}
	if ev := int64(800); res["e"] != ev {
		t.Errorf("b2: expect e=%v, got %v", ev, res["v"])
		return
	}
	// check memory:v
	av, ok, err = mem.Get(starlark.String("v"))
	if err != nil {
		t.Errorf("b2: expect nil error for memory:v, got %v", err)
		return
	}
	if ev := starlark.MakeInt(801); av != ev || !ok {
		t.Errorf("b2: expect v=%v, got %v", ev, av)
		return
	}
	// check memory:w
	av, ok, err = mem.Get(starlark.String("w"))
	if err != nil {
		t.Errorf("b2: expect nil error for memory:w, got %v", err)
		return
	}
	if ev := starlark.String("Aloha!"); av != ev || !ok {
		t.Errorf("b2: expect w=%v, got %v", ev, av)
		return
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single element",
			input:    []string{"a"},
			expected: []string{"a"},
		},
		{
			name:     "multiple elements",
			input:    []string{"a", "b", "c", "a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty string",
			input:    []string{"", "", "a", "b", "c", "a", "b", "c", ""},
			expected: []string{"", "a", "b", "c"},
		},
		{
			name:     "mixed case",
			input:    []string{"A", "b", "C", "a", "B", "c"},
			expected: []string{"A", "B", "C", "a", "b", "c"},
		},
		{
			name:     "no duplicates",
			input:    []string{"a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"b", "a", "c", "b", "a", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "already sorted",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "not sorted",
			input:    []string{"c", "b", "a"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "partially sorted",
			input:    []string{"a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with empty strings",
			input:    []string{"", "a", "", "b", "a"},
			expected: []string{"", "a", "b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := uniqueStrings(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected length %d, got %d", len(tc.expected), len(result))
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("Expected element %d to be %s, got %s", i, tc.expected[i], v)
				}
			}
		})
	}
}

func TestRemoveUniques(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		remove   []string
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			remove:   nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []string{},
			remove:   []string{},
			expected: []string{},
		},
		{
			name:     "empty remove",
			input:    []string{"a", "b", "c"},
			remove:   []string{},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "remove all",
			input:    []string{"a", "b", "c"},
			remove:   []string{"a", "b", "c"},
			expected: []string{},
		},
		{
			name:     "remove some",
			input:    []string{"a", "b", "c", "d"},
			remove:   []string{"a", "c"},
			expected: []string{"b", "d"},
		},
		{
			name:     "remove none",
			input:    []string{"a", "b", "c", "d"},
			remove:   []string{},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "remove duplicates",
			input:    []string{"a", "b", "c", "a", "b", "c"},
			remove:   []string{"a", "b", "c"},
			expected: []string{},
		},
		{
			name:     "remove single string",
			input:    []string{"a", "b", "c"},
			remove:   []string{"b"},
			expected: []string{"a", "c"},
		},
		{
			name:     "remove non-existent string",
			input:    []string{"a", "b", "c"},
			remove:   []string{"z"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "remove from empty slice",
			input:    []string{},
			remove:   []string{"a"},
			expected: []string{},
		},
		{
			name:     "input with duplicates, remove some",
			input:    []string{"a", "a", "b", "c", "c"},
			remove:   []string{"a", "c", "a", "c"},
			expected: []string{"b"},
		},
		{
			name:     "input with duplicates, remove non-existent",
			input:    []string{"a", "a", "b", "c", "c"},
			remove:   []string{"d", "e"},
			expected: []string{"a", "b", "c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := removeUniques(tc.input, tc.remove...)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected length %d, got %d", len(tc.expected), len(result))
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("Expected element %d to be %s, got %s", i, tc.expected[i], v)
				}
			}
		})
	}
}

func TestAppendUniques(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		append   []string
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			append:   nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []string{},
			append:   []string{},
			expected: []string{},
		},
		{
			name:     "empty append",
			input:    []string{},
			append:   []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "append empty",
			input:    []string{"a", "b", "c"},
			append:   []string{},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "append some",
			input:    []string{"a", "b", "c"},
			append:   []string{"d", "e", "f"},
			expected: []string{"a", "b", "c", "d", "e", "f"},
		},
		{
			name:     "append duplicates",
			input:    []string{"a", "b", "c"},
			append:   []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "append with duplicates to non-empty",
			input:    []string{"a", "b"},
			append:   []string{"c", "c", "d", "d"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "input and append with duplicates",
			input:    []string{"a", "a", "b", "b"},
			append:   []string{"b", "b", "c", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "append duplicates to empty",
			input:    []string{},
			append:   []string{"a", "a", "b", "b"},
			expected: []string{"a", "b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := appendUniques(tc.input, tc.append...)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected length %d, got %d", len(tc.expected), len(result))
			}
			t.Log(result, tc.expected)
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("Expected element %d to be %s, got %s", i, tc.expected[i], v)
				}
			}
		})
	}
}
