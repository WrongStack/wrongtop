BINARY := wrongtop
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/ersinkoc/wrongtop/cmd/wrongtop.version=$(VERSION)
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

cross:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64       ./cmd/wrongtop
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-x86_64     ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=linux   $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64   ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=linux   $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-x86_64  ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=windows $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-x86_64.exe ./cmd/wrongtop
	CGO_ENABLED=0 GOOS=windows $(GO) build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-aarch64.exe ./cmd/wrongtop

clean:
	rm -rf dist $(BINARY)
