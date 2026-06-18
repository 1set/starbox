MAKEFLAGS := --print-directory
SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c

# go dev
GOCMD=go
GOTEST=$(GOCMD) test

.PHONY: default ci test bench
default:
	@echo "targets: make test | make ci | make bench"

# CI bar: race + coverage profile (+ bench compile when present)
ci:
	$(GOTEST) -v -race -cover -covermode=atomic -coverprofile=coverage.txt -count 1 ./...
	$(GOTEST) -v -parallel=4 -run="none" -benchtime="2s" -benchmem -bench=. ./...

test:
	$(GOTEST) -v -race -cover -covermode=atomic -count 1 ./...

bench:
	$(GOTEST) -parallel=4 -run="none" -benchtime="2s" -benchmem -bench=. ./...
