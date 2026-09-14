# argo-tui developer commands.
# Go toolchain: pinned go 1.25.x (go.mod toolchain go1.25.4). This Makefile
# covers local builds and tests only.

GO ?= go
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
LDFLAGS ?= -X argo-tui/internal/buildinfo.Commit=$(COMMIT)

.PHONY: build test test-race vet lint-fmt smoke clean

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

smoke: build
	./dist/argo-tui --version
	@printf 'argo-tui: non-demo start must refuse\n'; ./dist/argo-tui >/dev/null 2>&1; \
	if [ $$? -ne 1 ]; then echo "expected exit 1 without --demo"; exit 1; fi; \
	echo "smoke OK"

clean:
	rm -rf dist
