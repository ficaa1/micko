# argo-tui developer commands.
# Go toolchain: pinned go 1.25.x (go.mod toolchain go1.25.4). This Makefile
# covers local builds and tests only.

GO ?= go
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
LDFLAGS ?= -X github.com/ficaa1/argo-tui/internal/buildinfo.Commit=$(COMMIT)

.PHONY: build test test-race vet lint-fmt smoke demo clean

# Build the binary into dist/.
build:
	$(GO) build -ldflags "$(LDFLAGS)" -o dist/ ./cmd/argo-tui

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
	./dist/argo-tui --version
	@printf 'argo-tui: an unknown profile must be refused\n'; \
	./dist/argo-tui --config /dev/null --profile no-such-profile >/dev/null 2>&1; \
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

clean:
	rm -rf dist
