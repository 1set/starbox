package starbox

import (
	"fmt"
	"os"
	"sort"

	"go.starlark.net/starlark"

	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv"
	"github.com/h2so5/here"
)

const (
	memoryTypeName = "collective_memory"
)

// NewMemory creates a new shared dictionary for la mémoire collective.
func NewMemory() *dataconv.SharedDict {
	return dataconv.NewNamedSharedDict(memoryTypeName)
}

// AttachMemory adds a shared dictionary to the global environment before execution.
func (s *Starbox) AttachMemory(name string, memory *dataconv.SharedDict) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add memory after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	s.globals[name] = memory
}

// CreateMemory creates a new shared dictionary for la mémoire collective with the given name, and adds it to the global environment before execution.
func (s *Starbox) CreateMemory(name string) *dataconv.SharedDict {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasExec {
		log.DPanic("cannot add memory after execution")
	}
	if s.globals == nil {
		s.globals = make(starlet.StringAnyMap)
	}
	memory := dataconv.NewNamedSharedDict(memoryTypeName)
	s.globals[name] = memory
	return memory
}

var (
	// HereDoc returns unindented string as here-document.
	HereDoc = here.Doc
	// HereDocf returns formatted unindented string as here-document.
	HereDocf = here.Docf
)

// HERE GOES THE INTERNALS

// eprintln likes fmt.Println but use stderr as the output.
func eprintln(a ...interface{}) (n int, err error) {
	return fmt.Fprintln(os.Stderr, a...)
}

// uniqueStrings returns a new slice of strings with duplicates removed and sorted.
func uniqueStrings(ss []string) []string {
	if len(ss) < 2 {
		return ss
	}
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	unique := make([]string, 0, len(m))
	for s := range m {
		unique = append(unique, s)
	}
	sort.Strings(unique)
	return unique
}

// removeUniques removes the given strings from the slice of strings.
// It returns a new slice of strings without the removed strings or duplicates.
func removeUniques(ss []string, removes ...string) []string {
	// create a map to track strings to remove
	remove := make(map[string]bool)
	for _, r := range removes {
		remove[r] = true
	}

	// create a map to track strings already added to the output slice
	seen := make(map[string]bool)
	output := make([]string, 0, len(ss))
	for _, s := range ss {
		if !remove[s] && !seen[s] {
			seen[s] = true
			output = append(output, s)
		}
	}
	return output
}

// appendUniques appends the given strings to the slice of strings, if not already exists.
// It returns a new slice of strings without duplicates.
func appendUniques(ss []string, appends ...string) []string {
	seen := make(map[string]bool)                     // tracks seen strings
	output := make([]string, 0, len(ss)+len(appends)) // preallocate memory for the output slice

	// add unique strings from the original slice to the output slice
	for _, s := range ss {
		if !seen[s] {
			output = append(output, s)
			seen[s] = true
		}
	}

	// append new unique strings to the output slice
	for _, s := range appends {
		if !seen[s] {
			output = append(output, s)
			seen[s] = true
		}
	}
	return output
}

// starlarkStringList converts a slice of strings to a list of starlark.Values.
func starlarkStringList(ss []string) *starlark.List {
	if len(ss) == 0 {
		return starlark.NewList(nil)
	}
	values := make([]starlark.Value, len(ss))
	for i, s := range ss {
		values[i] = starlark.String(s)
	}
	return starlark.NewList(values)
}
