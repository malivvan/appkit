.PHONY: all build vet test test-short cover lint lint-bsd cross cross-bsd dist js-check check-imports fmt tidy demo clean

all: build vet test check-imports cross js-check

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

lint-bsd:
	CGO_ENABLED=0 GOOS=freebsd golangci-lint run ./...
	CGO_ENABLED=0 GOOS=netbsd golangci-lint run ./...

cross: cross-bsd
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...
	GOOS=darwin GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=arm64 go build ./...
	GOOS=linux GOARCH=arm64 go build ./...

cross-bsd:
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build ./...
	CGO_ENABLED=0 GOOS=netbsd GOARCH=amd64 go build ./...

dist:
	mkdir -p ./build && rm -f ./build/*
	GOOS=windows GOARCH=amd64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_windows_amd64.exe ./demo/
	GOOS=windows GOARCH=arm64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_windows_arm64.exe ./demo/
	GOOS=windows GOARCH=386         go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_windows_386.exe ./demo/
	GOOS=darwin  GOARCH=amd64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_darwin_amd64 ./demo/
	GOOS=darwin  GOARCH=arm64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_darwin_arm64 ./demo/
	GOOS=linux   GOARCH=amd64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_amd64 ./demo/
	GOOS=linux   GOARCH=arm64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_arm64 ./demo/
	GOOS=linux   GOARCH=386         go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_386 ./demo/
	GOOS=linux   GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_armv7 ./demo/
	GOOS=linux   GOARCH=arm GOARM=6 go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_armv6 ./demo/
	GOOS=linux   GOARCH=arm GOARM=5 go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_armv5 ./demo/
	GOOS=linux   GOARCH=loong64     go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_loong64 ./demo/
	GOOS=linux   GOARCH=ppc64le     go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_ppc64le ./demo/
	GOOS=linux   GOARCH=riscv64     go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_riscv64 ./demo/
	GOOS=linux   GOARCH=s390x       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_linux_s390x ./demo/
	GOOS=freebsd GOARCH=amd64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_freebsd_amd64 ./demo/
	GOOS=freebsd GOARCH=arm64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_freebsd_arm64 ./demo/
	GOOS=netbsd  GOARCH=amd64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_netbsd_amd64 ./demo/
	GOOS=netbsd  GOARCH=arm64       go build -trimpath -ldflags="-s -w -buildid=" -o ./build/appkit_netbsd_arm64 ./demo/

js-check:
	@command -v node >/dev/null 2>&1 || { echo "js-check: node is required (the injected JS is parsed and behavior-tested with it)"; exit 1; }
	go test -run 'Script|Bridge' -count=1 .

check-imports:
	@forbidden="$$(go list -f '{{range .Imports}}{{println .}}{{end}}{{range .TestImports}}{{println .}}{{end}}{{range .XTestImports}}{{println .}}{{end}}' ./... | sort -u | grep -E '^(net/http|crypto/tls)$$')"; \
	if [ -n "$$forbidden" ]; then \
		echo "check-imports: forbidden stdlib imports found:"; \
		echo "$$forbidden"; \
		exit 1; \
	fi; \
	echo "check-imports: ok (no net/http or crypto/tls imports)"

fmt:
	gofmt -w .

tidy:
	go mod tidy

demo:
	mkdir -p ./build && go build -trimpath -o ./build/demo ./demo/

clean:
	rm -rf ./build
	rm -rf ./coverage.out
