BIN := lazymerge
PREFIX ?= /usr/local

.PHONY: all build run test vet fmt tidy install uninstall clean

all: fmt vet test build

build:
	go build -o $(BIN) .

run: build
	./$(BIN)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

install: build
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
