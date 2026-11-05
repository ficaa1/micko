# micko developer commands.
# Go toolchain: pinned go 1.25.x (go.mod toolchain go1.25.4). This Makefile
# covers local builds and tests only.

GO ?= go
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
LDFLAGS ?= -X github.com/ficaa1/micko/internal/buildinfo.Commit=$(COMMIT)

.PHONY: build test test-race vet lint-fmt smoke demo web clean

# Build the binary into dist/.
build:
	$(GO) build -ldflags "$(LDFLAGS)" -o dist/ ./cmd/micko

# Focused developer test set.
test:
	$(GO) test ./internal/core ./internal/config ./internal/app ./internal/ui/shared ./internal/testkit

# Race-enabled focused tests require a supported platform and C toolchain.
test-race:
	$(GO) test -race ./internal/core ./internal/config ./internal/app ./internal/ui/shared ./internal/testkit

vet:
	$(GO) vet ./...

lint-fmt:
	@out=$$($(GO)fmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# A profile that no config file names is refused before anything starts, so
# this runs without a terminal. Starting with no arguments opens the profile
# picker, which needs one.
smoke: build
	./dist/micko --version
	@printf 'micko: an unknown profile must be refused\n'; \
	./dist/micko --config /dev/null --profile no-such-profile >/dev/null 2>&1; \
	if [ $$? -ne 1 ]; then echo "expected exit 1 for an unknown profile"; exit 1; fi; \
	echo "smoke OK"

# Re-record the README GIF from docs/demo.tape. Needs vhs on PATH with a ttyd
# and ffmpeg it agrees with, and on macOS the terminal running it needs Screen
# Recording permission: vhs takes its frames through a headless browser, and
# without that permission every frame comes back empty and no file is written.
#
# The demo workflow records the same tape on a runner with a pinned toolchain,
# which is the path that does not depend on any of that.
demo: build
	vhs docs/demo.tape

# Build the browser demo into site/demo/: micko.wasm.gz and the wasm_exec.js
# of the same Go toolchain. bubbletea has no js platform file, so this copies
# the version go.mod resolves to dist/bubbletea, adds
# cmd/micko-web/_bubbletea/platform_js.go, and builds against the copy through
# a replace in dist/web.mod. go.mod and the native build do not change.
WEB_OUT ?= site/demo
web:
	@mkdir -p dist
	@# go list names the module's directory only once it is downloaded.
	$(GO) mod download charm.land/bubbletea/v2
	@chmod -R u+w dist/bubbletea 2>/dev/null; rm -r -f dist/bubbletea; \
		cp -r "$$($(GO) list -m -f '{{.Dir}}' charm.land/bubbletea/v2)" dist/bubbletea && \
		chmod -R u+w dist/bubbletea && \
		cp cmd/micko-web/_bubbletea/platform_js.go dist/bubbletea/
	cp go.mod dist/web.mod
	cp go.sum dist/web.sum
	$(GO) mod edit -modfile=dist/web.mod -replace charm.land/bubbletea/v2=./dist/bubbletea
	GOOS=js GOARCH=wasm $(GO) build -modfile=dist/web.mod -trimpath \
		-ldflags "-s -w $(LDFLAGS)" -o dist/micko.wasm ./cmd/micko-web
	gzip -9 -n -c dist/micko.wasm > $(WEB_OUT)/micko.wasm.gz
	install -m 0644 "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" $(WEB_OUT)/wasm_exec.js

clean:
	rm -rf dist
