SHELL := /usr/bin/env bash
GO_VERSION = 1.25
GOROOT =
PATH := ${PWD}/cache/go/bin:${PWD}/cache/go/misc/wasm:${PATH}
GOOS = js
GOARCH = wasm
export
LINT_VERSION=1.52.2

.PHONY: lint-deps
lint-deps: go
	@if ! which golangci-lint >/dev/null || [[ "$$(golangci-lint version 2>&1)" != *${LINT_VERSION}* ]]; then \
		curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v${LINT_VERSION}; \
	fi

.PHONY: lint
lint: lint-deps
	golangci-lint run

.PHONY: lint-fix
lint-fix: lint-deps
	golangci-lint run --fix

.PHONY: test-native
test-native:
	GOARCH= GOOS= go test \
		-race \
		-coverprofile=cover.out \
		./...

.PHONY: test-js
test-js: go
	go test \
		-coverprofile=cover_js.out \
		./...

.PHONY: test
test: test-native #test-js  # TODO restore when this is resolved: https://travis-ci.community/t/goos-js-goarch-wasm-go-run-fails-panic-newosproc-not-implemented/1651

.PHONY: go-static
go-static: build/go.tar.gz commands

build:
	mkdir -p build

build/go.tar.gz: build go
	GOARCH=$$(go env GOHOSTARCH) GOOS=$$(go env GOHOSTOS) \
		go run ./internal/cmd/gozip cache/go > build/go.tar.gz

.PHONY: clean
clean:
	rm -rf ./out ./build

cache:
	mkdir -p cache

.PHONY: commands
commands: build/wasm_exec.js build/go.wasm $(patsubst cmd/%,build/%.wasm,$(wildcard cmd/*))

.PHONY: go
go: cache/go${GO_VERSION}

cache/go${GO_VERSION}: cache
	if [[ ! -e cache/go${GO_VERSION} ]]; then \
		set -ex; \
		TMP=$$(mktemp -d); trap 'rm -rf "$$TMP"' EXIT; \
		git clone \
			--depth 1 \
			--single-branch \
			--branch dev \
			https://github.com/guymor4/go.git \
			"$$TMP"; \
		pushd "$$TMP/src"; \
		./make.bash; \
		export PATH="$$TMP/bin:$$PATH"; \
		go version; \
		mkdir -p ../bin/js_wasm; \
		go build -o ../bin/js_wasm/ std cmd/go cmd/gofmt; \
		go tool dist test -rebuild -list; \
		go build -o ../pkg/tool/js_wasm/ std cmd/buildid cmd/pack cmd/cover cmd/vet; \
		go install ./...; \
		popd; \
		mv "$$TMP" cache/go${GO_VERSION}; \
		ln -sfn go${GO_VERSION} cache/go; \
	fi
	touch cache/go${GO_VERSION}
	touch cache/go.mod  # Makes it so linters will ignore this dir

build/%.wasm: build go
	go build -o $@ ./cmd/$*

build/go.wasm: build go
	go build -o build/go.wasm .

build/wasm_exec.js: go
	cp cache/go/lib/wasm/wasm_exec.js build/wasm_exec.js
