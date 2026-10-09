SHELL := /bin/bash

GO ?= go
PREFIX ?= /usr/local
DESTDIR ?=
VERSION ?=
LDFLAGS := -s -w $(if $(VERSION),-X github.com/mattt/things-cli/internal/version.version=$(VERSION))
SOURCES := $(shell find cmd internal -name '*.go') main.go go.mod go.sum
FUZZTIME ?= 10s

HELPER := dist/Things CLI Callback.app
ifdef THINGS_SIGNING_IDENTITY
CODESIGN_FLAGS := --options runtime --timestamp --sign "$(THINGS_SIGNING_IDENTITY)"
else
CODESIGN_FLAGS := --sign -
endif

ifneq ($(filter notarize,$(MAKECMDGOALS)),)
ifndef THINGS_SIGNING_IDENTITY
$(error Set THINGS_SIGNING_IDENTITY to your Developer ID Application identity)
endif
ifndef THINGS_NOTARY_PROFILE
$(error Set THINGS_NOTARY_PROFILE to a notarytool Keychain profile name)
endif
endif

.PHONY: all build install uninstall clean setup generate fmt lint vet test fuzz benchmark demo helper notarize
all: things
build: things

things: $(SOURCES)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ .

install: things
	install -d '$(DESTDIR)$(PREFIX)/bin'
	install -m 0755 things '$(DESTDIR)$(PREFIX)/bin/things'

uninstall:
	rm -f '$(DESTDIR)$(PREFIX)/bin/things'

clean:
	rm -f things
	rm -rf dist

setup:
	$(GO) mod download
	command -v clang-format >/dev/null || brew install clang-format

generate:
	$(GO) generate ./...

fmt:
	gofmt -w .
	clang-format -i helper/main.m

lint: vet
	@unformatted="$$(gofmt -l .)"; \
	if [[ -n "$$unformatted" ]]; then printf 'Run make fmt for:\n%s\n' "$$unformatted" >&2; exit 1; fi
	clang-format --dry-run --Werror helper/main.m

vet:
	$(GO) vet ./...

test: things
	THINGS_TEST_BINARY='$(CURDIR)/things' $(GO) tool gotestsum --format testname -- -race ./...

fuzz:
	$(GO) test -run '^$$' -fuzz FuzzURLRoundTrip -fuzztime $(FUZZTIME) ./internal/urlscheme
	$(GO) test -run '^$$' -fuzz FuzzStructuredRequest -fuzztime $(FUZZTIME) ./internal/domain

benchmark: things
	THINGS_TEST_BINARY='$(CURDIR)/things' $(GO) test -run '^$$' -bench . -benchmem ./internal/service ./cmd

# The demo reads a synthetic library and records docs/demo.gif.
demo: things
	vhs docs/demo.tape

# The callback app bundles the AppKit URL-event bridge with the Go executable.
helper: things
	mkdir -p "$(HELPER)/Contents/MacOS"
	cp helper/Info.plist "$(HELPER)/Contents/Info.plist"
	cp things "$(HELPER)/Contents/MacOS/things"
	xcrun clang -fobjc-arc -mmacosx-version-min=12.0 -framework AppKit \
		helper/main.m -o "$(HELPER)/Contents/MacOS/callback-bridge"
	codesign --force $(CODESIGN_FLAGS) "$(HELPER)/Contents/MacOS/things"
	codesign --force $(CODESIGN_FLAGS) "$(HELPER)"

notarize: helper
	codesign --verify --deep --strict "$(HELPER)"
	ditto -c -k --keepParent "$(HELPER)" dist/things-callback.zip
	xcrun notarytool submit dist/things-callback.zip \
		--keychain-profile "$(THINGS_NOTARY_PROFILE)" --wait
	xcrun stapler staple "$(HELPER)"
	xcrun stapler validate "$(HELPER)"
	ditto -c -k --keepParent "$(HELPER)" dist/things-callback.zip
