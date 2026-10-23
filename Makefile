GO ?= go
BIN := bin

.PHONY: build test test-nocgo test-hardware vet mock web app clean

## build: build the binaries into bin/ (d75scope arrives in M1)
build:
	$(GO) build -o $(BIN)/d75probe ./cmd/d75probe

## test: all tests, with and without cgo (no radio needed)
test: vet test-nocgo
	$(GO) test ./...

test-nocgo:
	CGO_ENABLED=0 $(GO) test ./...

## test-hardware: tests that need the radio plugged in
test-hardware:
	$(GO) test -tags hardware -v ./audio ./radio

vet:
	$(GO) vet ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build ./...

## mock: run the probe against the fake radio and synthetic IF
mock: build
	$(BIN)/d75probe --mock --allow-writes --seconds 2 --out probe-out-mock

web:
	@echo "web: the front end arrives in M1"

app:
	@echo "app: the signed .app wrapper arrives in M5"

clean:
	rm -rf $(BIN) probe-out-*
