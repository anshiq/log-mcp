BINARY := agent-runtime
BIN_DIR := bin

.PHONY: build test race vet fmt tidy clean install snapshot proto gen-deps

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/agent-runtime
	go build -o $(BIN_DIR)/agentd ./cmd/agentd
	go build -o $(BIN_DIR)/agent-runtime-shim ./cmd/agent-runtime-shim
	go build -o $(BIN_DIR)/agent-runtime-gui ./cmd/agent-runtime-gui

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

install:
	go install ./cmd/agent-runtime
	go install ./cmd/agentd
	go install ./cmd/agent-runtime-shim

snapshot:
	goreleaser release --snapshot --clean

proto:
	buf generate
	@echo "Protobuf generated files are in gen/ and ui/src/gen/"

gen-deps:
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/bufbuild/buf/cmd/protoc-gen-go@latest
	go install github.com/bufbuild/buf/cmd/protoc-gen-connect-go@latest
	go install github.com/bufbuild/buf/cmd/protoc-gen-es@latest
	@echo "Protobuf tooling installed"

ci-check: build vet race test proto
	@echo "All checks passed"

.PHONY: proto gen-deps ci-check
