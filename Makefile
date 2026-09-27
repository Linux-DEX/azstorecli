.PHONY: run build tidy fmt vet test

run:
	go run ./cmd/azstore

build:
	go build -o bin/azstore ./cmd/azstore

tidy:
	GOFLAGS=-mod=mod go mod tidy

fmt:
	gofmt -l -w .

vet:
	go vet ./...

test:
	go test ./...
