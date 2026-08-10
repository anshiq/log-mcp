BINARY := agent-runtime
BIN_DIR := bin

.PHONY: build test race vet fmt tidy clean install

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/agent-runtime

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

install:
	go install ./cmd/agent-runtime
