# argo-tui developer commands (0.2.0-beta.1 local candidate).
# Go toolchain: pinned go 1.25.x (go.mod toolchain go1.25.4). CI/E1 owns
# pipeline wiring; this Makefile covers local builds and tests only.

GO ?= go

.PHONY: build test test-race vet lint-fmt smoke clean

# Build the binary into dist/.
build:
	$(GO) build -o dist/ ./cmd/argo-tui

# Focused developer test set.
test:
	$(GO) test ./internal/core ./internal/config ./internal/app ./internal/ui/shared ./internal/testkit

# Race-enabled run over the same packages. Note (F1 recovery): the toolchain
# here is a bare distribution without the race runtime, so `go test -race`
# fails with "unable to find C runtime" unless a C toolchain is present;
# record the environment gap for integration/QA instead of claiming a pass.
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
