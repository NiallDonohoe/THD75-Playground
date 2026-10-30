GO ?= go
BIN := bin

.PHONY: build test test-nocgo test-web test-ft8-reference test-hardware vet mock aprs-demo sat-demo web app clean

## build: build the binaries into bin/ (d75scope arrives in M1)
build:
	$(GO) build -o $(BIN)/d75probe ./cmd/d75probe
	$(GO) build -o $(BIN)/d75aprs ./cmd/d75aprs
	$(GO) build -o $(BIN)/d75ft8 ./cmd/d75ft8
	$(GO) build -o $(BIN)/d75sat ./cmd/d75sat

## test: all tests, with and without cgo (no radio needed)
test: vet test-nocgo test-web
	$(GO) test ./...

test-nocgo:
	CGO_ENABLED=0 $(GO) test ./...

## test-## sat-demo: run d75sat on made-up satellites; open http://127.0.0.1:8078/
sat-demo: build
	$(BIN)/d75sat --demo

web: the pages' map and sky maths, with Node's built-in runner (skipped without Node)
test-web:
	@if command -v node >/dev/null; then node --test web/*/*.test.mjs; else echo "test-web: no node, skipped"; fi

## test-ft8-reference: decode recordings that have WSJT-X's decodes alongside
## (FT8_REFERENCE_DIR, default testdata/ft8; skipped when there are none)
test-ft8-reference:
	$(GO) test -v -run TestDecodeReferenceRecordings ./ft8

## test-hardware: tests that need the radio plugged in
test-hardware:
	$(GO) test -tags hardware -v ./audio ./radio

vet:
	$(GO) vet ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build ./...

## mock: run the probe against the fake radio and synthetic IF
mock: build
	$(BIN)/d75probe --mock --allow-writes --seconds 2 --out probe-out-mock

## aprs-demo: run d75aprs on made-up traffic; open http://127.0.0.1:8076/
aprs-demo: build
	$(BIN)/d75aprs --demo

web:
	@echo "web: the front end arrives in M1"

app:
	@echo "app: the signed .app wrapper arrives in M5"

clean:
	rm -rf $(BIN) probe-out-*
