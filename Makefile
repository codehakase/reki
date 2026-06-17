BINARY  := reki
PKG     := ./...
TARGETS := linux/amd64 linux/arm64 darwin/arm64
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test fmt vet vuln tidy cross clean

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/reki

test:
	go test -race $(PKG)

fmt:
	gofmt -l -w .

vet:
	go vet $(PKG)

vuln:
	govulncheck $(PKG)

tidy:
	go mod tidy

cross:
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-$$os-$$arch ./cmd/reki; \
	done

clean:
	rm -rf bin dist
