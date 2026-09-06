BINARY := wrongtop
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# main.version (not the import path): Go 1.27's linker only accepts the
# main.* form for the main package — the long form silently no-ops.
LDFLAGS := -s -w -X main.version=$(VERSION)
GO ?= go

.PHONY: build run test vet lint cross clean

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/wrongtop

run: build
	./$(BINARY)

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

lint:
ifeq ($(shell command -v golangci-lint 2>/dev/null),)
	@echo "golangci-lint not found; falling back to go vet"
	$(GO) vet ./...
else
	golangci-lint run
endif

cross:
	# darwin builds link IOKit (cgo) for AppleSMC sensors; the native arch
	# uses the host SDK directly, the other needs the matching -arch support
	CGO_ENABLED=1 $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64       ./cmd/wrongtop
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64-nocgo ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=linux   $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64   ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=linux   $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-x86_64  ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=windows $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-x86_64.exe ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=windows $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-aarch64.exe ./cmd/wrongtop

clean:
	rm -rf dist $(BINARY)
