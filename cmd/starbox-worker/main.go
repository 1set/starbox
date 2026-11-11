package main

import (
	"io"
	"os"

	"github.com/1set/starbox/worker"
)

func run(args []string, in io.Reader, out, diagnostics io.Writer) int {
	return worker.Main(args, in, out, diagnostics)
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
