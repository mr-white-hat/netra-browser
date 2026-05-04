BIN      := netra-browser
PKG      := ./cmd/netra-browser
PREFIX   ?= /usr/local
BINDIR   := $(PREFIX)/bin
GO       ?= go
GOFLAGS  ?=
LDFLAGS  ?=

.PHONY: all build test e2e lint fmt clean install uninstall

all: build

build:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

test:
	$(GO) test ./...

e2e:
	$(GO) test -tags e2e ./e2e/...

lint:
	$(GO) vet ./...
	gofmt -l . | tee /dev/stderr | (! grep .)

fmt:
	gofmt -w .

clean:
	rm -f $(BIN) $(BIN).exe coverage.out
	$(GO) clean -testcache

install: build
	install -d $(BINDIR)
	install -m 0755 $(BIN) $(BINDIR)/$(BIN)

uninstall:
	rm -f $(BINDIR)/$(BIN)
