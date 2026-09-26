BINARY := agent-runtime
BIN_DIR := bin

.PHONY: build test race vet fmt tidy clean install snapshot proto gen-deps ui ui-web build-gui ci-check

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/agent-runtime
	go build -o $(BIN_DIR)/agentd ./cmd/agentd
	go build -o $(BIN_DIR)/agent-runtime-shim ./cmd/agent-runtime-shim

# ui builds the shared frontend (GUI + web). The GUI shell embeds a copy.
ui:
	cd ui && npm install --no-audit --no-fund && npm run build

ui-web:
	cd ui && npm run build:web

ui-build:
	@if [ -d ui/dist ]; then mkdir -p cmd/agent-runtime-gui/dist && cp -r ui/dist/. cmd/agent-runtime-gui/dist/; fi

# agent-runtime-gui needs cgo + GTK/WebKitGTK dev headers, so it's kept out
# of the default `build` target. On NixOS: nix develop ./packaging/nix -c make build-gui
build-gui: ui ui-build
	go build -tags desktop,production,webkit2_41 -o $(BIN_DIR)/agent-runtime-gui ./cmd/agent-runtime-gui

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
	rm -rf cmd/agent-runtime-gui/dist

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

ci-check: build vet race test proto
	@echo "All checks passed"
