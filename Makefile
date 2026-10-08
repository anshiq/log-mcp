BINARY := agent-runtime
BIN_DIR := bin
PREFIX ?= $(HOME)/.local

.PHONY: build test race vet fmt tidy clean install snapshot proto gen-deps ui ui-web ui-web-embed ui-check ci-check

build:
	@if [ -d ui/dist-web ]; then $(MAKE) ui-web-embed; fi
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/agent-runtime
	go build -o $(BIN_DIR)/agentd ./cmd/agentd
	go build -o $(BIN_DIR)/agent-runtime-shim ./cmd/agent-runtime-shim

# ui builds the web frontend (embedded by agentd in internal/webui/dist).
ui:
	cd ui && npm install --no-audit --no-fund && npm run build

ui-web:
	cd ui && npm run build
	$(MAKE) ui-web-embed

ui-web-embed:
	@if [ -d ui/dist-web ]; then \
		rm -rf internal/webui/dist; \
		mkdir -p internal/webui/dist; \
		cp -r ui/dist-web/. internal/webui/dist/; \
		printf '{"rev":"%s","dirty":%s,"built":"%s"}\n' "$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" "$$(git diff --quiet 2>/dev/null && echo false || echo true)" "$$(date -u +%Y-%m-%dT%H:%M:%SZ)" > internal/webui/dist/.build.json; \
	fi

ui-check:
	cd ui && npm run check && npm test && npm run build

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
	rm -rf gen/
	rm -rf ui/src/gen/
	rm -rf ui/dist ui/dist-web

install:
	go install ./cmd/agent-runtime
	go install ./cmd/agentd
	go install ./cmd/agent-runtime-shim

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
