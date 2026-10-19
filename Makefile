# rcplug-toolkit: the computer-side tools for rcplug and the RØDECaster Duo:
# setup window, firmware download and patching, flashing over USB, install,
# uninstall and diagnostics. Pure Go standard library. Run "make help".

GO       ?= go
VERSION  := $(shell cat VERSION)
DIST     := dist
HOSTARCH := $(shell $(GO) env GOARCH)
HOSTOS   := $(shell $(GO) env GOOS)
LDFLAGS  := -s -w -X main.version=$(VERSION)

TOOLS         := rcplug-setup rcplug-cfw rcplug-flash
SHELL_SCRIPTS := installer/rcplug-wizard.sh installer/rcplug-remote.sh tools/installer-test.sh tools/release-version.sh

# The rcplug platform repository, for the installer test and the device
# package in the Windows zip (until the toolkit downloads it from releases).
RCPLUG         ?= ../rcplug
RCPLUG_VERSION ?= $(shell cat $(RCPLUG)/VERSION 2>/dev/null)
PAYLOAD        ?= $(RCPLUG)/dist/rcplug-$(RCPLUG_VERSION)-aarch64.tar.gz

.PHONY: help all check fmt fmt-check vet lint test test-race test-wasm web-test test-windows installer-test docs-check \
        build package site clean

help: ## show this help
	@grep -hE '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-15s %s\n", $$1, $$2}'

all: check package ## run all checks and build the release files

check: fmt-check vet test-race test-wasm web-test lint docs-check ## everything CI runs (except the Windows and installer tests)

fmt: ## format the Go code
	gofmt -w .

fmt-check: ## fail if Go code is not formatted
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "not gofmt'ed:"; echo "$$out"; exit 1; fi

vet: ## go vet, also as Windows code (it has its own flash path) and as WebAssembly (the web pages' patcher)
	$(GO) vet ./...
	GOOS=windows $(GO) vet ./...
	GOOS=js GOARCH=wasm $(GO) vet ./cmd/rcplug-web ./internal/cfw ./internal/fwimage

lint: ## shellcheck the shell scripts
	shellcheck $(SHELL_SCRIPTS)

docs-check: ## every relative link and #anchor in the Markdown files resolves
	$(GO) run ./tools/doclinks

test: ## unit tests
	$(GO) test ./...

test-race: ## unit tests with the race detector
	$(GO) test -race ./...

# Go's runner for WebAssembly takes at most 8 KB of environment: run it with
# just what go needs (its runner moved to lib/wasm in Go 1.24).
test-wasm: ## the patcher's own tests compiled to WebAssembly, under Node (needs node)
	@root="$$($(GO) env GOROOT)"; run="$$root/lib/wasm/go_js_wasm_exec"; [ -x "$$run" ] || run="$$root/misc/wasm/go_js_wasm_exec"; \
	env -i PATH="$$PATH" HOME="$$HOME" TMPDIR="$${TMPDIR:-/tmp}" GOPATH="$$($(GO) env GOPATH)" GOMODCACHE="$$($(GO) env GOMODCACHE)" \
	  GOCACHE="$$($(GO) env GOCACHE)" GOPROXY="$$($(GO) env GOPROXY)" GOFLAGS="$$($(GO) env GOFLAGS)" GOTOOLCHAIN="$$($(GO) env GOTOOLCHAIN)" \
	  GOOS=js GOARCH=wasm $(GO) test -exec="$$run" ./internal/cfw ./internal/fwimage

web-test: ## the web pages' tests (needs node and ssh-keygen)
	node --test tools/webtest/*.test.mjs

site: ## build the web pages into $(DIST)/site, as GitHub Pages serves them
	$(GO) run ./tools/sitegen -out $(DIST)/site

test-windows: ## flash and setup tests as Windows binaries under Wine (needs wine)
	GOOS=windows GOARCH=amd64 $(GO) test -exec wine ./internal/flash ./cmd/rcplug-setup

installer-test: build ## install, update, add a plugin and uninstall in a local sandbox (needs $(RCPLUG))
	$(MAKE) -C $(RCPLUG) DIST=$(abspath $(DIST))/rcplug package-host
	RCPLUG_REPO=$(RCPLUG) tools/installer-test.sh $(abspath $(DIST))/rcplug/payload-host $(DIST)/$(HOSTOS)-$(HOSTARCH)/rcplug-setup

build: ## build the tools for this machine into dist/<os>-<arch>/
	@mkdir -p $(DIST)/$(HOSTOS)-$(HOSTARCH)
	@for c in $(TOOLS); do echo "  $(HOSTOS)/$(HOSTARCH) $$c"; \
	  CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(HOSTOS)-$(HOSTARCH)/$$c ./cmd/$$c || exit 1; done

package: ## release files in dist/: setup programs, firmware tool, flasher, wizard, Windows zip, SHA256SUMS
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; set -e; mkdir -p $(DIST); \
	cp installer/rcplug-wizard.sh installer/rcplug-remote.sh $(DIST)/; \
	for t in windows/amd64 darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do \
	  os=$${t%/*}; arch=$${t#*/}; sys=$$( [ $$os = darwin ] && echo macos || echo $$os ); \
	  case $$os in windows) exe=.exe;; *) exe=;; esac; \
	  name=rcplug-setup-$(VERSION)-$$sys-$$arch; \
	  [ $$os = windows ] && name=$$tmp/rcplug-setup.exe || name=$(DIST)/$$name; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $$name ./cmd/rcplug-setup; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/rcplug-cfw-$(VERSION)-$$sys-$$arch$$exe ./cmd/rcplug-cfw; \
	  case $$os in linux|windows) CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/rcplug-flash-$(VERSION)-$$sys-$$arch$$exe ./cmd/rcplug-flash;; esac; done; \
	cp $(DIST)/rcplug-flash-$(VERSION)-windows-amd64.exe $$tmp/rcplug-flash.exe; \
	cp installer/rcplug-setup.cmd installer/rcplug-remote.sh installer/WINDOWS.txt LICENSE THIRD_PARTY_NOTICES.md $$tmp/; \
	cp internal/flash/scripts/LICENSE $$tmp/LICENSE-rodecaster-duo-ssh; \
	payload=""; if [ -f "$(PAYLOAD)" ]; then payload="$(PAYLOAD)"; else echo "note: no device package at $(PAYLOAD); the Windows zip leaves it out"; fi; \
	$(GO) run ./tools/mkzip $(DIST)/rcplug-toolkit-$(VERSION)-windows.zip $$tmp/rcplug-setup.exe $$tmp/rcplug-flash.exe $$tmp/rcplug-setup.cmd \
	  $$tmp/rcplug-remote.sh $$tmp/WINDOWS.txt $$tmp/LICENSE $$tmp/THIRD_PARTY_NOTICES.md $$tmp/LICENSE-rodecaster-duo-ssh $$payload; \
	cd $(DIST) && sha256sum rcplug-toolkit-$(VERSION)-windows.zip rcplug-setup-$(VERSION)-* rcplug-cfw-$(VERSION)-* rcplug-flash-$(VERSION)-* \
	  rcplug-wizard.sh rcplug-remote.sh > SHA256SUMS
	@echo "packaged: $(DIST)/rcplug-toolkit-$(VERSION)-windows.zip and the tools for macOS and Linux (+ SHA256SUMS)"

clean: ## remove dist/
	rm -rf $(DIST)
