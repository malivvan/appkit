.PHONY: all build vet test test-short cover lint cross fmt tidy demo clean

all: build vet test lint cross

build:
	go build ./...

vet:
	go vet ./...

test:
	go test -timeout 180s ./...

test-short:
	go test -short ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

cross:
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...
	GOOS=darwin GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=arm64 go build ./...
	GOOS=linux GOARCH=arm64 go build ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

demo:
	mkdir -p ./build && go build -trimpath -o ./build/demo ./demo/

clean:
	rm -rf ./build
	rm -rf ./coverage.out
