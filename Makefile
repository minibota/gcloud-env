BINARY := gcloud-env
CMD := ./cmd/gcloud-env

.PHONY: build install test fmt tidy clean

build:
	go build -trimpath -ldflags="-s -w" -o bin/$(BINARY) $(CMD)

install:
	go install $(CMD)

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

tidy:
	go mod tidy

clean:
	rm -rf bin
