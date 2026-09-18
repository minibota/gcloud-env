BINARY := gcloud-env
CMD := ./cmd/gcloud-env
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test vet release fmt tidy clean

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

install:
	go install -trimpath -ldflags="$(LDFLAGS)" $(CMD)

test:
	go test ./...

vet:
	go vet ./...

release:
	./scripts/release.sh "$(VERSION)"

fmt:
	gofmt -w ./cmd ./internal

tidy:
	go mod tidy

clean:
	rm -rf bin releases
