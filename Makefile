BINARY := agent-runtime
BIN_DIR := bin
PREFIX ?= $(HOME)/.local
GUI_TAGS := desktop,production,webkit2_41

.PHONY: build test race vet fmt tidy clean install install-gui snapshot proto gen-deps ui ui-web ui-web-embed build-gui ui-check ci-check

build:
	@if [ -d ui/dist-web ]; then $(MAKE) ui-web-embed; fi
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/agent-runtime
	go build -o $(BIN_DIR)/agentd ./cmd/agentd
	go build -o $(BIN_DIR)/agent-runtime-shim ./cmd/agent-runtime-shim
	CGO_ENABLED=0 go build -o $(BIN_DIR)/agent-runtime-gui ./cmd/agent-runtime-gui

# ui builds the shared frontend: `build` (desktop, embedded by the GUI in
# internal/gui/dist) and `build:web` (web, embedded by agentd in internal/webui/dist).
ui:
	cd ui && npm install --no-audit --no-fund && npm run build

ui-web:
	cd ui && npm run build:web
	$(MAKE) ui-web-embed

ui-web-embed:
	@if [ -d ui/dist-web ]; then \
		rm -rf internal/webui/dist; \
		mkdir -p internal/webui/dist; \
		cp -r ui/dist-web/. internal/webui/dist/; \
		printf '{"rev":"%s","dirty":%s,"built":"%s"}\n' "$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" "$$(git diff --quiet 2>/dev/null && echo false || echo true)" "$$(date -u +%Y-%m-%dT%H:%M:%SZ)" > internal/webui/dist/.build.json; \
	fi

ui-build:
	@if [ -d ui/dist ]; then \
		rm -rf internal/gui/dist; \
		mkdir -p internal/gui/dist; \
		cp -r ui/dist/. internal/gui/dist/; \
		printf '{"rev":"%s","dirty":%s,"built":"%s"}\n' "$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" "$$(git diff --quiet 2>/dev/null && echo false || echo true)" "$$(date -u +%Y-%m-%dT%H:%M:%SZ)" > internal/gui/dist/.build.json; \
	fi

# The desktop build of the single `agent-runtime` binary (native GUI window)
# needs cgo + GTK/WebKitGTK dev headers, so it's kept out of the default
# `build` target. On NixOS: nix develop ./packaging/nix -c make build-gui
build-gui: ui ui-build ui-web-embed
	go build -ldflags "-X agent-runtime/internal/gui.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo v0.4.0-dev)" -tags "$(GUI_TAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/agent-runtime
	CGO_ENABLED=0 go build -o $(BIN_DIR)/agent-runtime-gui ./cmd/agent-runtime-gui

ui-check:
	cd ui && npm run check && npm test && npm run build && npm run build:web

test:
	go test $$(go list ./... | grep -v /cmd/agent-runtime-gui)

race:
	go test -race $$(go list ./... | grep -v /cmd/agent-runtime-gui)

vet:
	go vet $$(go list ./... | grep -v /cmd/agent-runtime-gui)

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
	rm -rf gen/
	rm -rf ui/src/gen/
	rm -rf ui/dist ui/dist-web
	rm -rf internal/gui/dist

install:
	go install ./cmd/agent-runtime
	go install ./cmd/agentd
	go install ./cmd/agent-runtime-shim
	CGO_ENABLED=0 go install ./cmd/agent-runtime-gui

install-gui: build-gui
	install -Dm755 $(BIN_DIR)/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	install -Dm755 $(BIN_DIR)/agent-runtime-gui $(DESTDIR)$(PREFIX)/bin/agent-runtime-gui
	install -Dm644 packaging/desktop/agent-runtime.desktop $(DESTDIR)$(PREFIX)/share/applications/agent-runtime.desktop

snapshot:
	goreleaser release --snapshot --clean

proto:
	PATH="$(PWD)/ui/node_modules/.bin:$$(go env GOPATH)/bin:$$PATH" buf generate
	buf lint
	buf breaking --against '.git#branch=master'
	@echo "Protobuf generated files are in gen/ and ui/src/gen/"

gen-deps:
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/bufbuild/buf/cmd/protoc-gen-go@latest
	go install github.com/bufbuild/buf/cmd/protoc-gen-connect-go@latest
	go install github.com/bufbuild/buf/cmd/protoc-gen-es@latest
	@echo "Protobuf tooling installed"

ci-check: build vet race test proto ui-check
	@echo "All checks passed"
